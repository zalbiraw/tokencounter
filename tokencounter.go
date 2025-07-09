// Package tokencounter a token counter plugin for OpenAI Chat Completion API.
package tokencounter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

// Config the plugin configuration.
type Config struct {
	RequestTokenHeader  string `json:"requestTokenHeader,omitempty"`
	ResponseTokenHeader string `json:"responseTokenHeader,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		RequestTokenHeader:  "X-Request-Token-Count",
		ResponseTokenHeader: "X-Response-Token-Count",
	}
}

// TokenCounter a token counter plugin.
type TokenCounter struct {
	next                http.Handler
	requestTokenHeader  string
	responseTokenHeader string
	name                string
}

// New creates a new TokenCounter plugin.
func New(ctx context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if config.RequestTokenHeader == "" {
		config.RequestTokenHeader = "X-Request-Token-Count"
	}
	if config.ResponseTokenHeader == "" {
		config.ResponseTokenHeader = "X-Response-Token-Count"
	}

	return &TokenCounter{
		next:                next,
		requestTokenHeader:  config.RequestTokenHeader,
		responseTokenHeader: config.ResponseTokenHeader,
		name:                name,
	}, nil
}

// MessageContent represents content in messages.
type MessageContent any

// TextContent represents text content.
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ImageContent represents image content.
type ImageContent struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url"`
}

// Message represents a message in the conversation.
type Message struct {
	Role    string         `json:"role"`
	Content MessageContent `json:"content,omitempty"`
	Name    string         `json:"name,omitempty"`
}

// ResponseFormat represents the response format configuration.
type ResponseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Schema      map[string]any `json:"schema"`
		Strict      bool           `json:"strict,omitempty"`
	} `json:"json_schema,omitempty"`
}

// SimpleRequest represents minimal OpenAI request fields needed for token counting.
type SimpleRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Usage represents token usage information.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`     //nolint:tagliatelle
	CompletionTokens int `json:"completion_tokens"` //nolint:tagliatelle
	TotalTokens      int `json:"total_tokens"`      //nolint:tagliatelle
}

// SimpleResponse represents a minimal OpenAI response for token counting.
type SimpleResponse struct {
	Usage Usage `json:"usage"`
}

type responseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	headers    http.Header
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return len(b), nil
}

func (rw *responseWriter) Header() http.Header {
	if rw.headers == nil {
		rw.headers = make(http.Header)
	}
	return rw.headers
}

func (tc *TokenCounter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		tc.next.ServeHTTP(rw, req)
		return
	}

	if !strings.Contains(req.URL.Path, "/chat/completions") {
		tc.next.ServeHTTP(rw, req)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		tc.next.ServeHTTP(rw, req)
		return
	}
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var openAIReq SimpleRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		tc.next.ServeHTTP(rw, req)
		return
	}

	respWriter := &responseWriter{
		ResponseWriter: rw,
		body:           &bytes.Buffer{},
		statusCode:     http.StatusOK,
		headers:        make(http.Header),
	}

	tc.next.ServeHTTP(respWriter, req)

	// Handle non-successful responses
	if respWriter.statusCode != http.StatusOK {
		// Copy captured headers to actual response
		for key, values := range respWriter.headers {
			for _, value := range values {
				rw.Header().Add(key, value)
			}
		}
		rw.WriteHeader(respWriter.statusCode)
		_, _ = rw.Write(respWriter.body.Bytes())
		return
	}

	// Parse OpenAI response (only usage field)
	var simpleResp SimpleResponse
	if err := json.Unmarshal(respWriter.body.Bytes(), &simpleResp); err != nil {
		// Copy captured headers to actual response
		for key, values := range respWriter.headers {
			for _, value := range values {
				rw.Header().Add(key, value)
			}
		}
		rw.WriteHeader(respWriter.statusCode)
		_, _ = rw.Write(respWriter.body.Bytes())
		return
	}

	// Copy all original headers to the actual response
	for key, values := range respWriter.headers {
		for _, value := range values {
			rw.Header().Add(key, value)
		}
	}

	// Check if this is a cache hit - if so, use estimated tokens since OpenAI returns 0
	cacheStatus := respWriter.Header().Get("X-Cache-Status")

	var finalBody []byte
	if cacheStatus == "Hit" {
		requestTokens := tc.countRequestTokens(&openAIReq)
		responseTokens := tc.estimateResponseTokensFromBody(respWriter.body.Bytes())
		tc.setEstimatedTokens(rw, &openAIReq, respWriter.body.Bytes())

		// Update the usage in the response body
		finalBody = tc.updateUsageInResponse(respWriter.body.Bytes(), requestTokens, responseTokens)
	} else {
		// Use actual token counts from OpenAI response
		tc.setActualTokens(rw, &simpleResp)
		finalBody = respWriter.body.Bytes()
	}

	// Write the response
	rw.WriteHeader(respWriter.statusCode)
	_, _ = rw.Write(finalBody)
}

func (tc *TokenCounter) setEstimatedTokens(rw http.ResponseWriter, req *SimpleRequest, respBody []byte) {
	requestTokens := tc.countRequestTokens(req)
	responseTokens := tc.estimateResponseTokensFromBody(respBody)
	rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(requestTokens))
	rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(responseTokens))
}

func (tc *TokenCounter) setActualTokens(rw http.ResponseWriter, resp *SimpleResponse) {
	if resp.Usage.PromptTokens > 0 {
		rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(resp.Usage.PromptTokens))
	}
	if resp.Usage.CompletionTokens > 0 {
		rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(resp.Usage.CompletionTokens))
	}
}

func (tc *TokenCounter) countRequestTokens(req *SimpleRequest) int {
	totalTokens := 0

	for _, message := range req.Messages {
		totalTokens += tc.estimateTokensFromContent(message.Content)
		totalTokens += tc.estimateTokens(message.Role)
		totalTokens += 4
	}

	totalTokens += tc.estimateTokens(req.Model)
	totalTokens += 2

	return totalTokens
}

// FullResponse represents the complete OpenAI response structure for parsing content.
type FullResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (tc *TokenCounter) estimateResponseTokensFromBody(respBody []byte) int {
	var fullResp FullResponse
	if err := json.Unmarshal(respBody, &fullResp); err != nil {
		return 0
	}

	totalTokens := 0
	for _, choice := range fullResp.Choices {
		totalTokens += tc.estimateTokens(choice.Message.Content)
	}

	return totalTokens
}

func (tc *TokenCounter) updateUsageInResponse(respBody []byte, promptTokens, completionTokens int) []byte {
	// Parse the response as a generic map to preserve all fields
	var responseMap map[string]any
	if err := json.Unmarshal(respBody, &responseMap); err != nil {
		return respBody
	}

	// Update the usage field
	if usage, exists := responseMap["usage"]; exists {
		if usageMap, ok := usage.(map[string]any); ok {
			usageMap["prompt_tokens"] = promptTokens
			usageMap["completion_tokens"] = completionTokens
			usageMap["total_tokens"] = promptTokens + completionTokens
		}
	}

	// Marshal back to JSON
	updatedBody, err := json.Marshal(responseMap)
	if err != nil {
		return respBody
	}

	return updatedBody
}

func (tc *TokenCounter) estimateTokensFromContent(content MessageContent) int {
	if content == nil {
		return 0
	}

	switch c := content.(type) {
	case string:
		return tc.estimateTokens(c)
	case []any:
		totalTokens := 0
		for _, item := range c {
			if itemMap, ok := item.(map[string]any); ok {
				if itemType, exists := itemMap["type"]; exists && itemType == "text" {
					if text, textExists := itemMap["text"]; textExists {
						if textStr, ok := text.(string); ok {
							totalTokens += tc.estimateTokens(textStr)
						}
					}
				} else if itemType == "image_url" {
					totalTokens += 85
				}
			}
		}
		return totalTokens
	default:
		return 0
	}
}

func (tc *TokenCounter) estimateTokens(text string) int {
	if text == "" {
		return 0
	}

	text = strings.ToLower(text)

	words := 0
	inWord := false

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inWord {
				words++
				inWord = true
			}
		} else {
			inWord = false
		}
	}

	tokens := int(float64(words) * 1.33)

	if tokens == 0 && len(text) > 0 {
		tokens = 1
	}

	return tokens
}

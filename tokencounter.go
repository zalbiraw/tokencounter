// Package tokencounter a token counter plugin for OpenAI Chat Completion API.
package tokencounter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
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

// MessageContent represents different types of content in a message
type MessageContent interface{}

// TextContent represents text content
type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ImageContent represents image content
type ImageContent struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL    string `json:"url"`
		Detail string `json:"detail,omitempty"`
	} `json:"image_url"`
}

// ToolCall represents a tool call in a message
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// Message represents a message in the conversation
type Message struct {
	Role       string         `json:"role"`
	Content    MessageContent `json:"content,omitempty"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// ResponseFormat represents the response format configuration
type ResponseFormat struct {
	Type       string `json:"type"`
	JSONSchema *struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Schema      map[string]interface{} `json:"schema"`
		Strict      bool                   `json:"strict,omitempty"`
	} `json:"json_schema,omitempty"`
}

// Tool represents a tool available to the model
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description,omitempty"`
		Parameters  map[string]interface{} `json:"parameters,omitempty"`
	} `json:"function"`
}

// ToolChoice represents how the model should use tools
type ToolChoice interface{}

// SimpleRequest represents minimal OpenAI request fields needed for token counting
type SimpleRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`     //nolint:tagliatelle
	CompletionTokens int `json:"completion_tokens"` //nolint:tagliatelle
	TotalTokens      int `json:"total_tokens"`      //nolint:tagliatelle
}

// SimpleResponse represents a minimal OpenAI response for token counting
type SimpleResponse struct {
	Usage Usage `json:"usage"`
}

type responseWriter struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (tc *TokenCounter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		log.Printf("TokenCounter: bypassing non-POST request to %s\n", req.URL.Path)
		tc.next.ServeHTTP(rw, req)
		return
	}

	if !strings.Contains(req.URL.Path, "/chat/completions") {
		log.Printf("TokenCounter: bypassing non-chat-completions request to %s\n", req.URL.Path)
		tc.next.ServeHTTP(rw, req)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		log.Printf("TokenCounter: failed to read request body: %v\n", err)
		tc.next.ServeHTTP(rw, req)
		return
	}
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var openAIReq SimpleRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		log.Printf("TokenCounter: failed to parse OpenAI request: %v\n", err)
		tc.next.ServeHTTP(rw, req)
		return
	}

	respWriter := &responseWriter{
		ResponseWriter: rw,
		body:           &bytes.Buffer{},
		statusCode:     http.StatusOK,
	}

	tc.next.ServeHTTP(respWriter, req)

	// Handle non-successful responses
	if respWriter.statusCode != http.StatusOK {
		return
	}

	// Parse OpenAI response (only usage field)
	var simpleResp SimpleResponse
	if err := json.Unmarshal(respWriter.body.Bytes(), &simpleResp); err != nil {
		log.Printf("TokenCounter: failed to parse OpenAI response: %v\n", err)
		return
	}

	// Check if this is a cache hit - if so, use estimated tokens since OpenAI returns 0
	cacheStatus := respWriter.Header().Get("X-Cache-Status")
	log.Printf("TokenCounter: cache status = '%s'\n", cacheStatus)
	if cacheStatus == "Hit" {
		log.Printf("TokenCounter: using estimated tokens for cache hit\n")
		tc.setEstimatedTokens(respWriter, &openAIReq, &simpleResp)
	} else {
		log.Printf("TokenCounter: using actual tokens\n")
		// Use actual token counts from OpenAI response
		tc.setActualTokens(respWriter, &simpleResp)
	}
}

func (tc *TokenCounter) setEstimatedTokens(rw http.ResponseWriter, req *SimpleRequest, resp *SimpleResponse) {
	requestTokens := tc.countRequestTokens(req)
	responseTokens := tc.countResponseTokens(resp)
	log.Printf("TokenCounter: setting estimated tokens - request: %d, response: %d\n", requestTokens, responseTokens)
	rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(requestTokens))
	rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(responseTokens))
	log.Printf("TokenCounter: headers set - %s: %s, %s: %s\n", tc.requestTokenHeader, rw.Header().Get(tc.requestTokenHeader), tc.responseTokenHeader, rw.Header().Get(tc.responseTokenHeader))
}

func (tc *TokenCounter) setActualTokens(rw http.ResponseWriter, resp *SimpleResponse) {
	log.Printf("TokenCounter: setting actual tokens - prompt: %d, completion: %d\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if resp.Usage.PromptTokens > 0 {
		rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(resp.Usage.PromptTokens))
	}
	if resp.Usage.CompletionTokens > 0 {
		rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(resp.Usage.CompletionTokens))
	}
	log.Printf("TokenCounter: headers set - %s: %s, %s: %s\n", tc.requestTokenHeader, rw.Header().Get(tc.requestTokenHeader), tc.responseTokenHeader, rw.Header().Get(tc.responseTokenHeader))
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

func (tc *TokenCounter) countResponseTokens(resp *SimpleResponse) int {
	return resp.Usage.CompletionTokens
}

func (tc *TokenCounter) estimateTokensFromContent(content MessageContent) int {
	if content == nil {
		return 0
	}

	switch c := content.(type) {
	case string:
		return tc.estimateTokens(c)
	case []interface{}:
		totalTokens := 0
		for _, item := range c {
			if itemMap, ok := item.(map[string]interface{}); ok {
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

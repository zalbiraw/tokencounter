// Package tokencounter a token counter plugin for OpenAI Chat Completion API.
package tokencounter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
	Role         string          `json:"role"`
	Content      MessageContent  `json:"content,omitempty"`
	Name         string          `json:"name,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
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

// OpenAIRequest represents an OpenAI Chat Completion API request.
type OpenAIRequest struct {
	Model             string          `json:"model"`
	Messages          []Message       `json:"messages"`
	MaxTokens         *int            `json:"max_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	N                 *int            `json:"n,omitempty"`
	Stream            *bool           `json:"stream,omitempty"`
	Stop              interface{}     `json:"stop,omitempty"`
	PresencePenalty   *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty  *float64        `json:"frequency_penalty,omitempty"`
	LogitBias         map[string]int  `json:"logit_bias,omitempty"`
	User              string          `json:"user,omitempty"`
	ResponseFormat    *ResponseFormat `json:"response_format,omitempty"`
	Seed              *int            `json:"seed,omitempty"`
	Tools             []Tool          `json:"tools,omitempty"`
	ToolChoice        ToolChoice      `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool           `json:"parallel_tool_calls,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`     //nolint:tagliatelle
	CompletionTokens int `json:"completion_tokens"` //nolint:tagliatelle
	TotalTokens      int `json:"total_tokens"`      //nolint:tagliatelle
}

// Choice represents a completion choice
type Choice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message"`
	Delta        *Message `json:"delta,omitempty"`
	Logprobs     *struct {
		Content []struct {
			Token   string `json:"token"`
			Logprob float64 `json:"logprob"`
			Bytes   []int   `json:"bytes,omitempty"`
			TopLogprobs []struct {
				Token   string  `json:"token"`
				Logprob float64 `json:"logprob"`
				Bytes   []int   `json:"bytes,omitempty"`
			} `json:"top_logprobs"`
		} `json:"content"`
	} `json:"logprobs,omitempty"`
	FinishReason string `json:"finish_reason"`
}

// OpenAIResponse represents an OpenAI Chat Completion API response.
type OpenAIResponse struct {
	ID                string  `json:"id"`
	Object            string  `json:"object"`
	Created           int64   `json:"created"`
	Model             string  `json:"model"`
	SystemFingerprint string  `json:"system_fingerprint,omitempty"`
	Usage             Usage   `json:"usage"`
	Choices           []Choice `json:"choices"`
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
		_, _ = os.Stderr.WriteString(fmt.Sprintf("TokenCounter: bypassing non-POST request to %s\n", req.URL.Path))
		tc.next.ServeHTTP(rw, req)
		return
	}

	if !strings.Contains(req.URL.Path, "/chat/completions") {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("TokenCounter: bypassing non-chat-completions request to %s\n", req.URL.Path))
		tc.next.ServeHTTP(rw, req)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to read request body: %v\n", err))
		tc.next.ServeHTTP(rw, req)
		return
	}
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var openAIReq OpenAIRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to parse OpenAI request: %v\n", err))
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
		requestTokens := tc.countRequestTokens(&openAIReq)
		rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(requestTokens))
		return
	}

	// Parse OpenAI response
	var openAIResp OpenAIResponse
	if err := json.Unmarshal(respWriter.body.Bytes(), &openAIResp); err != nil {
		_, _ = os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to parse OpenAI response: %v\n", err))
		tc.setEstimatedTokens(rw, &openAIReq, &openAIResp)
		return
	}

	// Use actual token counts from OpenAI response
	tc.setActualTokens(rw, &openAIResp)
}

func (tc *TokenCounter) setEstimatedTokens(rw http.ResponseWriter, req *OpenAIRequest, resp *OpenAIResponse) {
	requestTokens := tc.countRequestTokens(req)
	responseTokens := tc.countResponseTokens(resp)
	rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(requestTokens))
	rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(responseTokens))
}

func (tc *TokenCounter) setActualTokens(rw http.ResponseWriter, resp *OpenAIResponse) {
	if resp.Usage.PromptTokens > 0 {
		rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(resp.Usage.PromptTokens))
	}
	if resp.Usage.CompletionTokens > 0 {
		rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(resp.Usage.CompletionTokens))
	}
}

func (tc *TokenCounter) countRequestTokens(req *OpenAIRequest) int {
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

func (tc *TokenCounter) countResponseTokens(resp *OpenAIResponse) int {
	if resp.Usage.CompletionTokens > 0 {
		return resp.Usage.CompletionTokens
	}

	totalTokens := 0
	for _, choice := range resp.Choices {
		totalTokens += tc.estimateTokensFromContent(choice.Message.Content)
	}

	return totalTokens
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

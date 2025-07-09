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

type OpenAIRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream,omitempty"`
}

type OpenAIResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
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
	if req.Method != "POST" {
		os.Stderr.WriteString(fmt.Sprintf("TokenCounter: bypassing non-POST request to %s\n", req.URL.Path))
		tc.next.ServeHTTP(rw, req)
		return
	}

	if !strings.Contains(req.URL.Path, "/chat/completions") {
		os.Stderr.WriteString(fmt.Sprintf("TokenCounter: bypassing non-chat-completions request to %s\n", req.URL.Path))
		tc.next.ServeHTTP(rw, req)
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to read request body: %v\n", err))
		tc.next.ServeHTTP(rw, req)
		return
	}
	req.Body = io.NopCloser(bytes.NewBuffer(body))

	var openAIReq OpenAIRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to parse OpenAI request: %v\n", err))
		tc.next.ServeHTTP(rw, req)
		return
	}

	requestTokens := tc.countRequestTokens(&openAIReq)
	
	respWriter := &responseWriter{
		ResponseWriter: rw,
		body:           &bytes.Buffer{},
		statusCode:     200,
	}

	tc.next.ServeHTTP(respWriter, req)

	if respWriter.statusCode == 200 {
		var openAIResp OpenAIResponse
		if err := json.Unmarshal(respWriter.body.Bytes(), &openAIResp); err != nil {
			os.Stderr.WriteString(fmt.Sprintf("TokenCounter: failed to parse OpenAI response: %v\n", err))
		} else {
			responseTokens := tc.countResponseTokens(&openAIResp)
			rw.Header().Set(tc.responseTokenHeader, strconv.Itoa(responseTokens))
		}
	}

	rw.Header().Set(tc.requestTokenHeader, strconv.Itoa(requestTokens))
}

func (tc *TokenCounter) countRequestTokens(req *OpenAIRequest) int {
	totalTokens := 0
	
	for _, message := range req.Messages {
		totalTokens += tc.estimateTokens(message.Content)
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
		totalTokens += tc.estimateTokens(choice.Message.Content)
	}
	
	return totalTokens
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

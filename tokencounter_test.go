package tokencounter

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestTokenCounter(t *testing.T) {
	cfg := CreateConfig()
	cfg.RequestTokenHeader = "X-Request-Tokens"
	cfg.ResponseTokenHeader = "X-Response-Tokens"

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		response := map[string]interface{}{
			"usage": map[string]interface{}{
				"prompt_tokens":     10,
				"completion_tokens": 20,
				"total_tokens":      30,
			},
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "Hello world",
					},
				},
			},
		}
		_ = json.NewEncoder(rw).Encode(response)
	})

	handler, err := New(ctx, next, cfg, "token-counter")
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()

	requestBody := map[string]interface{}{
		"model": "gpt-3.5-turbo",
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": "Hello world",
			},
		},
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBuffer(bodyBytes))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(recorder, req)

	requestTokens := recorder.Header().Get("X-Request-Tokens")
	responseTokens := recorder.Header().Get("X-Response-Tokens")

	if requestTokens == "" {
		t.Error("Expected request token header to be set")
	}

	if responseTokens == "" {
		t.Error("Expected response token header to be set")
	}

	if requestTokens != "" {
		if _, err := strconv.Atoi(requestTokens); err != nil {
			t.Error("Request token count should be a number")
		}
	}

	if responseTokens != "" {
		if _, err := strconv.Atoi(responseTokens); err != nil {
			t.Error("Response token count should be a number")
		}
	}
}

func TestTokenCounterBypassNonChatCompletions(t *testing.T) {
	cfg := CreateConfig()

	ctx := context.Background()
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	handler, err := New(ctx, next, cfg, "token-counter")
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	handler.ServeHTTP(recorder, req)

	requestTokens := recorder.Header().Get("X-Request-Token-Count")
	responseTokens := recorder.Header().Get("X-Response-Token-Count")

	if requestTokens != "" {
		t.Error("Expected no request token header for non-chat-completions endpoint")
	}

	if responseTokens != "" {
		t.Error("Expected no response token header for non-chat-completions endpoint")
	}
}

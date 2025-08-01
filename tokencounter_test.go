package tokencounter

// import (
// 	"bytes"
// 	"context"
// 	"encoding/json"
// 	"net/http"
// 	"net/http/httptest"
// 	"strconv"
// 	"testing"
// )

// func TestTokenCounter(t *testing.T) {
// 	cfg := CreateConfig()
// 	cfg.RequestTokenHeader = "X-Request-Tokens"
// 	cfg.ResponseTokenHeader = "X-Response-Tokens"

// 	ctx := context.Background()
// 	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
// 		response := map[string]interface{}{
// 			"usage": map[string]interface{}{
// 				"prompt_tokens":     10,
// 				"completion_tokens": 20,
// 				"total_tokens":      30,
// 			},
// 			"choices": []map[string]interface{}{
// 				{
// 					"message": map[string]interface{}{
// 						"content": "Hello world",
// 					},
// 				},
// 			},
// 		}
// 		if err := json.NewEncoder(rw).Encode(response); err != nil {
// 			t.Errorf("Failed to encode response: %v", err)
// 		}
// 	})

// 	handler, err := New(ctx, next, cfg, "token-counter")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	recorder := httptest.NewRecorder()

// 	requestBody := map[string]interface{}{
// 		"model": "gpt-3.5-turbo",
// 		"messages": []map[string]interface{}{
// 			{
// 				"role":    "user",
// 				"content": "Hello world",
// 			},
// 		},
// 	}
// 	bodyBytes, err := json.Marshal(requestBody)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBuffer(bodyBytes))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	req.Header.Set("Content-Type", "application/json")

// 	handler.ServeHTTP(recorder, req)

// 	requestTokens := recorder.Header().Get("X-Request-Tokens")
// 	responseTokens := recorder.Header().Get("X-Response-Tokens")

// 	if requestTokens == "" {
// 		t.Error("Expected request token header to be set")
// 	}

// 	if responseTokens == "" {
// 		t.Error("Expected response token header to be set")
// 	}

// 	if requestTokens != "" {
// 		if _, err := strconv.Atoi(requestTokens); err != nil {
// 			t.Error("Request token count should be a number")
// 		}
// 	}

// 	if responseTokens != "" {
// 		if _, err := strconv.Atoi(responseTokens); err != nil {
// 			t.Error("Response token count should be a number")
// 		}
// 	}
// }

// func TestTokenCounterBypassNonChatCompletions(t *testing.T) {
// 	cfg := CreateConfig()

// 	ctx := context.Background()
// 	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
// 		rw.WriteHeader(http.StatusOK)
// 	})

// 	handler, err := New(ctx, next, cfg, "token-counter")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	recorder := httptest.NewRecorder()

// 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/health", nil)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	handler.ServeHTTP(recorder, req)

// 	requestTokens := recorder.Header().Get("X-Request-Token-Count")
// 	responseTokens := recorder.Header().Get("X-Response-Token-Count")

// 	if requestTokens != "" {
// 		t.Error("Expected no request token header for non-chat-completions endpoint")
// 	}

// 	if responseTokens != "" {
// 		t.Error("Expected no response token header for non-chat-completions endpoint")
// 	}
// }

// func TestTokenCounterCacheHit(t *testing.T) {
// 	cfg := CreateConfig()
// 	cfg.RequestTokenHeader = "X-Request-Tokens"
// 	cfg.ResponseTokenHeader = "X-Response-Tokens"

// 	ctx := context.Background()
// 	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
// 		// Simulate cache hit with X-Cache-Status header
// 		rw.Header().Set("X-Cache-Status", "Hit")

// 		// Simulate cached response with zero token counts (typical for cache hits)
// 		response := map[string]interface{}{
// 			"usage": map[string]interface{}{
// 				"prompt_tokens":     0,
// 				"completion_tokens": 0,
// 				"total_tokens":      0,
// 			},
// 			"choices": []map[string]interface{}{
// 				{
// 					"message": map[string]interface{}{
// 						"content": "This is a cached response",
// 					},
// 				},
// 			},
// 		}
// 		if err := json.NewEncoder(rw).Encode(response); err != nil {
// 			t.Errorf("Failed to encode response: %v", err)
// 		}
// 	})

// 	handler, err := New(ctx, next, cfg, "token-counter")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	recorder := httptest.NewRecorder()

// 	requestBody := map[string]interface{}{
// 		"model": "gpt-3.5-turbo",
// 		"messages": []map[string]interface{}{
// 			{
// 				"role":    "user",
// 				"content": "What is the weather like?",
// 			},
// 		},
// 	}
// 	bodyBytes, err := json.Marshal(requestBody)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBuffer(bodyBytes))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	req.Header.Set("Content-Type", "application/json")

// 	handler.ServeHTTP(recorder, req)

// 	requestTokens := recorder.Header().Get("X-Request-Tokens")
// 	responseTokens := recorder.Header().Get("X-Response-Tokens")

// 	if requestTokens == "" {
// 		t.Error("Expected request token header to be set for cache hit")
// 	}

// 	if responseTokens == "" {
// 		t.Error("Expected response token header to be set for cache hit")
// 	}

// 	// Verify we get estimated tokens (not zero)
// 	if requestTokens == "0" {
// 		t.Error("Expected non-zero estimated request tokens for cache hit")
// 	}

// 	if responseTokens == "0" {
// 		t.Error("Expected non-zero estimated response tokens for cache hit")
// 	}
// }

// func TestTokenCounterCacheMiss(t *testing.T) {
// 	cfg := CreateConfig()
// 	cfg.RequestTokenHeader = "X-Request-Tokens"
// 	cfg.ResponseTokenHeader = "X-Response-Tokens"

// 	ctx := context.Background()
// 	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
// 		// Simulate cache miss with X-Cache-Status header
// 		rw.Header().Set("X-Cache-Status", "Miss")

// 		// Simulate normal response with actual token counts
// 		response := map[string]interface{}{
// 			"usage": map[string]interface{}{
// 				"prompt_tokens":     15,
// 				"completion_tokens": 25,
// 				"total_tokens":      40,
// 			},
// 			"choices": []map[string]interface{}{
// 				{
// 					"message": map[string]interface{}{
// 						"content": "This is a fresh response",
// 					},
// 				},
// 			},
// 		}
// 		if err := json.NewEncoder(rw).Encode(response); err != nil {
// 			t.Errorf("Failed to encode response: %v", err)
// 		}
// 	})

// 	handler, err := New(ctx, next, cfg, "token-counter")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	recorder := httptest.NewRecorder()

// 	requestBody := map[string]interface{}{
// 		"model": "gpt-3.5-turbo",
// 		"messages": []map[string]interface{}{
// 			{
// 				"role":    "user",
// 				"content": "What is the weather like?",
// 			},
// 		},
// 	}
// 	bodyBytes, err := json.Marshal(requestBody)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBuffer(bodyBytes))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	req.Header.Set("Content-Type", "application/json")

// 	handler.ServeHTTP(recorder, req)

// 	requestTokens := recorder.Header().Get("X-Request-Tokens")
// 	responseTokens := recorder.Header().Get("X-Response-Tokens")

// 	// Should use actual token counts from response
// 	if requestTokens != "15" {
// 		t.Errorf("Expected request tokens to be 15, got %s", requestTokens)
// 	}

// 	if responseTokens != "25" {
// 		t.Errorf("Expected response tokens to be 25, got %s", responseTokens)
// 	}
// }

// func TestTokenCounterNoCacheHeader(t *testing.T) {
// 	cfg := CreateConfig()
// 	cfg.RequestTokenHeader = "X-Request-Tokens"
// 	cfg.ResponseTokenHeader = "X-Response-Tokens"

// 	ctx := context.Background()
// 	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
// 		// No X-Cache-Status header set

// 		// Normal response with actual token counts
// 		response := map[string]interface{}{
// 			"usage": map[string]interface{}{
// 				"prompt_tokens":     12,
// 				"completion_tokens": 18,
// 				"total_tokens":      30,
// 			},
// 			"choices": []map[string]interface{}{
// 				{
// 					"message": map[string]interface{}{
// 						"content": "Normal response without cache header",
// 					},
// 				},
// 			},
// 		}
// 		if err := json.NewEncoder(rw).Encode(response); err != nil {
// 			t.Errorf("Failed to encode response: %v", err)
// 		}
// 	})

// 	handler, err := New(ctx, next, cfg, "token-counter")
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	recorder := httptest.NewRecorder()

// 	requestBody := map[string]interface{}{
// 		"model": "gpt-3.5-turbo",
// 		"messages": []map[string]interface{}{
// 			{
// 				"role":    "user",
// 				"content": "Hello world",
// 			},
// 		},
// 	}
// 	bodyBytes, err := json.Marshal(requestBody)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/v1/chat/completions", bytes.NewBuffer(bodyBytes))
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	req.Header.Set("Content-Type", "application/json")

// 	handler.ServeHTTP(recorder, req)

// 	requestTokens := recorder.Header().Get("X-Request-Tokens")
// 	responseTokens := recorder.Header().Get("X-Response-Tokens")

// 	// Should use actual token counts from response (normal behavior)
// 	if requestTokens != "12" {
// 		t.Errorf("Expected request tokens to be 12, got %s", requestTokens)
// 	}

// 	if responseTokens != "18" {
// 		t.Errorf("Expected response tokens to be 18, got %s", responseTokens)
// 	}
// }

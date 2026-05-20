package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicProviderHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, ожидался /v1/messages", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-test" {
			t.Errorf("отсутствует x-api-key")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("отсутствует anthropic-version")
		}
		var req anthropicRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.System != "сис" {
			t.Errorf("System не выделен из messages: %q", req.System)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("messages: %v", req.Messages)
		}
		if req.MaxTokens <= 0 {
			t.Errorf("MaxTokens=%d должен быть >0", req.MaxTokens)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Привет"}],"model":"claude-3"}`))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("claude", srv.URL, "sk-test")
	resp, err := p.Complete(context.Background(), ChatRequest{
		Model: "claude-3",
		Messages: []ChatMessage{
			{Role: "system", Content: "сис"},
			{Role: "user", Content: "хай"},
		},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Content != "Привет" {
		t.Errorf("content = %q", resp.Content)
	}
}

func TestAnthropicProviderNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := NewAnthropicProvider("c", srv.URL, "bad")
	_, err := p.Complete(context.Background(), ChatRequest{Model: "x"})
	if err == nil || !contains(err.Error(), "401") {
		t.Errorf("err = %v, ожидалось содержит 401", err)
	}
}

func TestAnthropicProviderEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[],"model":"x"}`))
	}))
	defer srv.Close()
	p := NewAnthropicProvider("c", srv.URL, "k")
	_, err := p.Complete(context.Background(), ChatRequest{Model: "x"})
	if err == nil {
		t.Error("ожидалась ошибка на пустой content")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

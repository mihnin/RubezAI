package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/config"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// discardTestLogger — slog без вывода (для тестов buildRouter).
func discardTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthcheckAt(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
	))
	defer healthy.Close()
	if code := healthcheckAt(healthy.URL); code != 0 {
		t.Errorf("healthcheckAt(200) = %d, ожидалось 0", code)
	}

	failing := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	))
	defer failing.Close()
	if code := healthcheckAt(failing.URL); code != 1 {
		t.Errorf("healthcheckAt(500) = %d, ожидалось 1", code)
	}

	if code := healthcheckAt("http://127.0.0.1:1/health"); code != 1 {
		t.Errorf("healthcheckAt(недоступен) = %d, ожидалось 1", code)
	}
}

func TestBuildRouter(t *testing.T) {
	providers := []storage.ModelProvider{
		{Name: "mock-1", Adapter: "mock", IsEnabled: true},
		{Name: "ext", Adapter: "openai_compatible", Endpoint: "http://x", IsEnabled: true},
		{Name: "off", Adapter: "mock", IsEnabled: false},
	}
	router := buildRouter(providers, "key", nil, discardTestLogger())
	if router.Count() != 2 {
		t.Errorf("Count = %d, ожидалось 2 (отключённый провайдер пропущен)", router.Count())
	}
	if !router.Has("mock-1") || !router.Has("ext") {
		t.Error("ожидаемые провайдеры не зарегистрированы")
	}
	if router.Has("off") {
		t.Error("отключённый провайдер не должен регистрироваться")
	}
}

func TestBuildRouterSelectsAdapterType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[{"message":`+
				`{"role":"assistant","content":"внешний ответ"}}]}`)
		}))
	defer server.Close()

	router := buildRouter([]storage.ModelProvider{
		{Name: "ext", Adapter: "openai_compatible", Endpoint: server.URL, IsEnabled: true},
		{Name: "mck", Adapter: "mock", IsEnabled: true},
	}, "key", nil, discardTestLogger())

	ext, err := router.Complete(context.Background(), "ext",
		llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "x"}}})
	if err != nil || ext.Content != "внешний ответ" {
		t.Errorf("openai-провайдер не выполнил HTTP-вызов: %q, %v", ext.Content, err)
	}
	mck, _ := router.Complete(context.Background(), "mck",
		llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "x"}}})
	if !strings.Contains(mck.Content, "[mock]") {
		t.Errorf("mock-провайдер не выбран: %q", mck.Content)
	}
}

func TestBuildRouterEmpty(t *testing.T) {
	if buildRouter(nil, "k", nil, discardTestLogger()).Count() != 0 {
		t.Error("пустой список провайдеров → пустой роутер")
	}
}

func TestBuildRouterUnknownAdapterFallsBackToMock(t *testing.T) {
	router := buildRouter([]storage.ModelProvider{
		{Name: "x", Adapter: "future_adapter", IsEnabled: true},
	}, "k", nil, discardTestLogger())
	resp, _ := router.Complete(context.Background(), "x",
		llm.ChatRequest{Messages: []llm.ChatMessage{{Role: "user", Content: "q"}}})
	if !strings.Contains(resp.Content, "[mock]") {
		t.Errorf("неизвестный адаптер должен давать mock, получено %q", resp.Content)
	}
}

func TestBuildEmbedderDefaultsToMock(t *testing.T) {
	cases := []config.EmbedderConfig{
		{Kind: ""},
		{Kind: "mock"},
	}
	for _, c := range cases {
		e, err := buildEmbedder(c)
		if err != nil {
			t.Fatalf("kind=%q: %v", c.Kind, err)
		}
		if e.Name() != "mock-sha256-v1" {
			t.Errorf("kind=%q: name=%q", c.Kind, e.Name())
		}
		if e.Dim() != llm.EmbeddingDim {
			t.Errorf("kind=%q: dim=%d", c.Kind, e.Dim())
		}
	}
}

func TestBuildEmbedderOpenAICompatible(t *testing.T) {
	e, err := buildEmbedder(config.EmbedderConfig{
		Kind: "openai_compatible", URL: "http://lm:1234",
		Model: "bge-m3", APIKey: "sk", Timeout: 10,
	})
	if err != nil {
		t.Fatalf("buildEmbedder: %v", err)
	}
	if e.Name() != "bge-m3" {
		t.Errorf("name = %q", e.Name())
	}
}

func TestBuildEmbedderRejectsOpenAIWithoutURL(t *testing.T) {
	_, err := buildEmbedder(config.EmbedderConfig{
		Kind: "openai_compatible", Model: "m", Timeout: 5,
	})
	if err == nil {
		t.Fatal("ожидалась ошибка при пустом URL")
	}
	if !strings.Contains(err.Error(), "EMBEDDER_URL") {
		t.Errorf("ошибка должна упоминать EMBEDDER_URL: %v", err)
	}
}

func TestBuildEmbedderRejectsOpenAIWithoutModel(t *testing.T) {
	_, err := buildEmbedder(config.EmbedderConfig{
		Kind: "openai_compatible", URL: "http://x", Timeout: 5,
	})
	if err == nil {
		t.Fatal("ожидалась ошибка при пустой Model")
	}
	if !strings.Contains(err.Error(), "EMBEDDER_MODEL") {
		t.Errorf("ошибка должна упоминать EMBEDDER_MODEL: %v", err)
	}
}

func TestBuildEmbedderRejectsUnknownKind(t *testing.T) {
	_, err := buildEmbedder(config.EmbedderConfig{Kind: "future-kind"})
	if err == nil {
		t.Fatal("ожидалась ошибка для unknown kind")
	}
}

func TestLogLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"error":   slog.LevelError,
		"info":    slog.LevelInfo,
		"unknown": slog.LevelInfo,
	}
	for input, want := range cases {
		if got := logLevel(input); got != want {
			t.Errorf("logLevel(%q) = %v, ожидалось %v", input, got, want)
		}
	}
}

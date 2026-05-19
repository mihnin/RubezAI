package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

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
	router := buildRouter(providers, "key")
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

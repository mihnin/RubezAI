package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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

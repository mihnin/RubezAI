// Package api содержит HTTP-слой сервиса rubezh-api.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter собирает HTTP-роутер сервиса.
func NewRouter(logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(logger))
	r.Get("/health", healthHandler)
	return r
}

// requestLogger — middleware структурного логирования запросов: метод, путь,
// статус ответа, длительность и request_id для сквозной трассировки.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)
			if r.URL.Path == "/health" {
				return // healthcheck-пробы не засоряют журнал
			}
			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.Status(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

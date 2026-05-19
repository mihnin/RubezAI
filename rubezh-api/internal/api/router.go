// Package api содержит HTTP-слой сервиса rubezh-api.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// Deps — зависимости HTTP-слоя.
type Deps struct {
	Logger     *slog.Logger
	Store      *storage.Storage
	AuthSecret string
}

// NewRouter собирает HTTP-роутер сервиса. Маршруты /api защищены
// auth-middleware; /health — публичная проба.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(deps.Logger))

	r.Get("/health", healthHandler)
	r.Route("/api", func(api chi.Router) {
		api.Use(auth.Middleware(deps.AuthSecret))
		api.Post("/policies/test", policyTestHandler)
		api.Get("/policies", listPoliciesHandler(deps.Store))
		api.Post("/policies", createPolicyHandler(deps.Store))
	})
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

// Package api содержит HTTP-слой сервиса rubezh-api.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// Deps — зависимости HTTP-слоя.
type Deps struct {
	Logger        *slog.Logger
	Store         *storage.Storage
	AuthSecret    string
	Router        *llm.Router          // LLM Router; используется /api/chat
	SanitizerURL  string               // базовый URL сервиса rubezh-sanitizer
	MappingCipher *crypto.Cipher       // AES-GCM для pseudonym_mappings; nil ⇒ mappings не пишутся (только для тестов)
	Minio         *storage.MinioClient // MinIO для документов (Итерация 10); nil ⇒ /api/documents 503
	// ReloadRouter — hot-reload LLM Router из БД (F2). nil ⇒ изменения
	// провайдеров видны только после restart api (только для тестов).
	ReloadRouter func(ctx context.Context) error
}

// NewRouter собирает HTTP-роутер сервиса. Маршруты /api защищены
// auth-middleware; /health — публичная проба.
//
// Возвращает (handler, orchestrator) — main.go вызывает orchestrator.Wait()
// при graceful shutdown, чтобы дождаться фоновых auto-incident-горутин
// (план iteration-9.md §Р4 + MAJOR-A финального ревью).
func NewRouter(deps Deps) (http.Handler, *chat.Orchestrator) {
	orchestrator := chat.NewOrchestrator(
		sanitizer.NewClient(deps.SanitizerURL), deps.Router, deps.Store,
		deps.MappingCipher)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(deps.Logger))

	r.Get("/health", healthHandler)
	// Публичный auth-endpoint: выпуск dev-токена. Вне auth-middleware.
	// docs/design/identity.md §«MVP auth-flow».
	r.Post("/api/auth/dev-login", devLoginHandler(deps.Store, deps.AuthSecret))
	r.Route("/api", func(api chi.Router) {
		api.Use(auth.Middleware(deps.AuthSecret))
		api.Post("/policies/test", policyTestHandler)
		api.Get("/policies", listPoliciesHandler(deps.Store))
		api.Post("/policies", createPolicyHandler(deps.Store))
		api.Get("/models", listModelsHandler(deps.Store))
		api.Post("/models", createModelHandler(
			deps.Store, deps.MappingCipher, deps.ReloadRouter, deps.Logger))
		api.Post("/models/{id}/api-key",
			updateModelAPIKeyHandler(
				deps.Store, deps.MappingCipher, deps.ReloadRouter, deps.Logger))
		api.Patch("/models/{id}",
			patchModelHandler(deps.Store, deps.ReloadRouter, deps.Logger))
		api.Delete("/models/{id}",
			deleteModelHandler(deps.Store, deps.ReloadRouter, deps.Logger))
		api.Get("/chat/sessions", listChatSessionsHandler(deps.Store))
		api.Post("/chat/sessions", createChatSessionHandler(deps.Store))
		api.Get("/chat/sessions/{id}/messages",
			listChatMessagesHandler(deps.Store))
		api.Post("/chat/messages/{id}/reveal",
			revealChatHandler(deps.Store, deps.MappingCipher, deps.Logger))
		api.Post("/chat/preview", previewChatHandler(
			orchestrator, deps.Store, deps.Router, deps.Logger))
		api.Post("/chat", chatHandler(
			orchestrator, deps.Store, deps.Router, deps.Logger))
		// Audit / Incidents — Итерация 9.
		api.Get("/audit-events", listAuditEventsHandler(deps.Store))
		api.Get("/audit-events/{id}", getAuditEventHandler(deps.Store))
		api.Post("/audit-events/export", exportAuditEventsHandler(deps.Store))
		// GET-вариант для скачивания через простую ссылку (фронт apiDownload).
		api.Get("/audit-events/export", exportAuditEventsHandler(deps.Store))
		api.Get("/incidents", listIncidentsHandler(deps.Store))
		api.Post("/incidents", createIncidentHandler(deps.Store))
		api.Get("/incidents/{id}", getIncidentHandler(deps.Store))
		api.Patch("/incidents/{id}", patchIncidentHandler(deps.Store))
		api.Get("/incidents/{id}/notes",
			listIncidentNotesHandler(deps.Store))
		api.Post("/incidents/{id}/notes",
			addIncidentNoteHandler(deps.Store))
		// Документы — Итерация 10.
		api.Post("/documents",
			uploadDocumentHandler(deps.Store, deps.Minio))
		api.Get("/documents", listDocumentsHandler(deps.Store))
		api.Get("/documents/{id}", getDocumentHandler(deps.Store))
		api.Get("/documents/{id}/chunks",
			listDocumentChunksHandler(deps.Store))
		api.Get("/documents/{id}/download",
			downloadDocumentHandler(deps.Store, deps.Minio))
		api.Get("/documents/{id}/masked",
			downloadMaskedDocumentHandler(deps.Store))
		api.Delete("/documents/{id}",
			deleteDocumentHandler(deps.Store, deps.Minio))
		api.Post("/documents/{id}/retry",
			retryDocumentHandler(deps.Store))
		// RAG поиск — Итерация 11.
		api.Post("/search", searchHandler(deps.Store,
			sanitizer.NewClient(deps.SanitizerURL)))
	})
	return r, orchestrator
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

// Package chat — оркестратор запросов /api/chat: sanitize → policy →
// route в LLM → проверка утечки → восстановление псевдонимов → аудит.
package chat

import (
	"context"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// SanitizerClient — зависимость: клиент сервиса обезличивания.
type SanitizerClient interface {
	Preview(ctx context.Context, req sanitizer.PreviewRequest) (sanitizer.PreviewResponse, error)
}

// LLMRouter — зависимость: маршрутизатор провайдеров LLM.
type LLMRouter interface {
	Complete(ctx context.Context, provider string, req llm.ChatRequest) (llm.ChatResponse, error)
}

// Store — зависимость: персистентность чата и аудита.
type Store interface {
	RecordChatRequest(ctx context.Context, rec storage.ChatRequestRecord) (storage.ChatRequestIDs, error)
	RecordChatTermination(ctx context.Context, rec storage.ChatTerminationRecord) (storage.ChatTerminationIDs, error)
	InsertAuditEvent(ctx context.Context, ev storage.AuditEvent) (string, error)
	// CreateAutoIncident — atomic Tx3 (INSERT incidents + INSERT audit_events).
	// План iteration-9.md §Р4. Возвращает (incident, auditEventID, err);
	// при дубликате — storage.ErrIncidentAutoDuplicate.
	CreateAutoIncident(ctx context.Context, inc storage.IncidentInput, ev storage.AuditEvent) (storage.Incident, string, error)
}

// EventSink — приёмник SSE-событий потока /api/chat. Fail принимает
// requestID, чтобы терминальное событие error (контракт chat.schema.json
// #SseError) всегда содержало коррелятор для расследования.
type EventSink interface {
	Meta(m MetaEvent) error
	// Status сообщает клиенту, на каком этапе сейчас находится запрос.
	// Это не контент модели, а безопасная телеметрия пайплайна: policy/RAG/
	// remote CLI/audit. Raw prompt, stderr и секреты сюда не попадают.
	Status(s StatusEvent) error
	Delta(content string) error
	// Done завершает поток. assistantMessageID — id записанного сообщения
	// ассистента (для последующего reveal); пуст для путей без записи.
	Done(requestID, assistantMessageID string) error
	Fail(message, requestID string) error
	// RagHits — метаданные источников retrieval'а (Итерация 11 §Р4 Ф4c).
	// Эмитится между Meta и первым Delta, если RAG включён и нашлись чанки.
	// snippet'ы НЕ отправляются (они уходят только в LLM-context); только
	// document_id / filename / chunk_index / relevance. Контракт —
	// docs/contracts/rag.schema.json#RagHitMeta.
	RagHits(requestID string, hits []RAGHit) error
}

// Request — подготовленный HTTP-слоем запрос чата: сессия, провайдер и
// пользователь уже провалидированы и разрешены.
type Request struct {
	RequestID  string
	SessionID  string
	UserID     string
	UserRole   string
	Message    string
	Provider   string // имя провайдера для llm.Router
	ProviderID string // id записи model_providers (для аудита и БД)
	ModelTrust string // trust_level провайдера
	Model      string // имя модели для запроса к LLM
	// SystemPrompt — опциональная системная инструкция для основной модели.
	// Задаётся из UI; пользовательский текст всё равно проходит sanitizer/policy.
	SystemPrompt string
	// PreviewToken — одноразовый токен закэшированного предпросмотра (J.0).
	// Если задан и валиден — Prepare переиспользует тот sanitize вместо нового.
	PreviewToken string
	// APIKeyOverride — персональный ключ пользователя к провайдеру (L); если
	// непуст, LLM вызывается с ним вместо org-ключа.
	APIKeyOverride string
	// RAG — параметры retrieval'а (Итерация 11 §Р4). nil или Enabled=false
	// → старое поведение без retrieval'а; при наличии — Stream врезает
	// шаги retrieval / risk-filter / policy re-evaluation между Meta и LLM.
	RAG *RAGParams
	// Review — серверная много-модельная ревизия ответа. Если задана,
	// черновик основной модели НЕ стримится клиенту: он остаётся на сервере,
	// последовательно проверяется указанными моделями, и только финальная
	// версия уходит в Delta.
	Review *ReviewParams
}

// ReviewProvider — одна модель-проверяющий для серверной ревизии.
type ReviewProvider struct {
	Name         string
	Model        string
	SystemPrompt string
}

// ReviewParams — параметры server-side ревизии ответа несколькими моделями.
type ReviewParams struct {
	Enabled   bool
	Providers []ReviewProvider
	// MaxRounds — максимум полных циклов проверки: reviewers -> primary edit.
	// 0 означает default в orchestrator_review.go.
	MaxRounds int
}

// RiskView — оценка риска для SSE-события meta.
type RiskView struct {
	Level   string
	Score   float64
	Classes []string
}

// MetaEvent — payload SSE-события meta. RequestID — коррелятор аудит-событий,
// дублируется в meta/done/error, чтобы пользователь имел id запроса в любой
// момент потока (см. chat.schema.json#SseMeta).
type MetaEvent struct {
	Decision  string
	Risk      RiskView
	Provider  string
	Reasons   []string
	RequestID string
}

// StatusEvent — payload SSE-события status. Событие промежуточное:
// клиент показывает его во время долгих стадий, особенно remote CLI.
type StatusEvent struct {
	RequestID string
	Stage     string
	Message   string
	Provider  string
	Model     string
}

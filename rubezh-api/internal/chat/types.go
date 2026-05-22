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
	Delta(content string) error
	// Done завершает поток. assistantMessageID — id записанного сообщения
	// ассистента (для последующего reveal); пуст для путей без записи.
	Done(requestID, assistantMessageID string) error
	Fail(message, requestID string) error
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
	// PreviewToken — одноразовый токен закэшированного предпросмотра (J.0).
	// Если задан и валиден — Prepare переиспользует тот sanitize вместо нового.
	PreviewToken string
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

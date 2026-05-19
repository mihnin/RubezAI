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
}

// EventSink — приёмник SSE-событий потока /api/chat.
type EventSink interface {
	Meta(m MetaEvent) error
	Delta(content string) error
	Done(requestID string) error
	Fail(message string) error
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
}

// RiskView — оценка риска для SSE-события meta.
type RiskView struct {
	Level   string
	Score   float64
	Classes []string
}

// MetaEvent — payload SSE-события meta.
type MetaEvent struct {
	Decision string
	Risk     RiskView
	Provider string
	Reasons  []string
}

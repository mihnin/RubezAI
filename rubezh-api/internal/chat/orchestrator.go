package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

const (
	_auditTimeout = 5 * time.Second
	_deltaRunes   = 80 // размер чанка псевдо-стриминга SSE
)

// Orchestrator выполняет сквозной поток запроса /api/chat.
type Orchestrator struct {
	sanitizer SanitizerClient
	llm       LLMRouter
	store     Store
}

// NewOrchestrator создаёт оркестратор с зависимостями.
func NewOrchestrator(s SanitizerClient, l LLMRouter, st Store) *Orchestrator {
	return &Orchestrator{sanitizer: s, llm: l, store: st}
}

// Handle выполняет поток: sanitize → policy → запись запроса → meta →
// route в LLM → проверка утечки → запись ответа → стрим. Ошибки потока
// сообщаются через sink (SSE error) и аудит; возвращаемая ошибка означает
// сбой записи в sink или аудит.
func (o *Orchestrator) Handle(
	ctx context.Context, req Request, sink EventSink,
) error {
	preview, err := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
		Text: req.Message, Context: "chat",
	})
	if err != nil {
		return o.fail(ctx, req, sink, "ошибка обезличивания запроса",
			o.errorEvent(req, map[string]any{"stage": "sanitize"}))
	}

	pmap, err := BuildPseudonymMap(req.Message, preview.Entities)
	if err != nil {
		return o.fail(ctx, req, sink, "ошибка карты псевдонимов",
			o.errorEvent(req, map[string]any{
				"stage": "pseudonym_map", "error": err.Error(),
			}))
	}

	outcome := policy.DefaultPolicy().Decide(
		ToPolicyInput(preview, req.ModelTrust, req.UserRole))

	if _, err := o.store.RecordChatRequest(
		ctx, o.requestRecord(req, preview, outcome)); err != nil {
		return o.fail(ctx, req, sink, "ошибка записи запроса",
			o.errorEvent(req, map[string]any{"stage": "record_request"}))
	}

	if err := sink.Meta(metaFor(req, preview, outcome)); err != nil {
		return fmt.Errorf("chat: отправка meta: %w", err)
	}

	act := actionFor(outcome.Decision, req.Message, preview.SanitizedText)
	if !act.callLLM {
		return o.finishBlocked(ctx, req, preview, outcome, sink)
	}
	return o.runLLM(ctx, req, preview, outcome, pmap, act, sink)
}

// runLLM вызывает провайдера, проверяет утечку, записывает ответ и стримит.
func (o *Orchestrator) runLLM(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, pmap PseudonymMap, act action, sink EventSink,
) error {
	resp, err := o.llm.Complete(ctx, req.Provider, llm.ChatRequest{
		Model:    req.Model,
		Messages: []llm.ChatMessage{{Role: "user", Content: act.sendText}},
	})
	if err != nil || resp.Content == "" {
		return o.fail(ctx, req, sink, "ошибка вызова модели",
			o.policyErrorEvent(req, preview, outcome,
				map[string]any{"stage": "llm"}))
	}

	leaked := pmap.DetectLeak(resp.Content)
	stored, streamed := finalTexts(act, pmap, resp.Content)

	if _, err := o.store.RecordChatTermination(ctx, o.terminationRecord(
		req, preview, outcome, "chat_response", stored, leaked)); err != nil {
		// ответ LLM получен, но не персистирован — delta не стримим
		return o.fail(ctx, req, sink, "ошибка записи ответа",
			o.policyErrorEvent(req, preview, outcome, map[string]any{
				"stage": "record_response", "llm_completed": true,
			}))
	}

	for _, chunk := range chunkText(streamed, _deltaRunes) {
		if err := sink.Delta(chunk); err != nil {
			return fmt.Errorf("chat: отправка delta: %w", err)
		}
	}
	return sink.Done(req.RequestID)
}

// finishBlocked обрабатывает deny/escalate: LLM не вызывается.
func (o *Orchestrator) finishBlocked(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, sink EventSink,
) error {
	notice := blockedNotice(outcome)
	if _, err := o.store.RecordChatTermination(ctx, o.terminationRecord(
		req, preview, outcome, "chat_blocked", notice, nil)); err != nil {
		return o.fail(ctx, req, sink, "ошибка записи блокировки",
			o.policyErrorEvent(req, preview, outcome,
				map[string]any{"stage": "record_blocked"}))
	}
	return sink.Done(req.RequestID)
}

// fail записывает chat_error (контекстом без отмены) и шлёт SSE error.
func (o *Orchestrator) fail(
	ctx context.Context, _ Request, sink EventSink,
	message string, ev storage.AuditEvent,
) error {
	auditCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx), _auditTimeout)
	defer cancel()
	_, auditErr := o.store.InsertAuditEvent(auditCtx, ev)
	if sinkErr := sink.Fail(message); sinkErr != nil {
		return fmt.Errorf("chat: отправка error: %w", sinkErr)
	}
	if auditErr != nil {
		return fmt.Errorf("chat: %s; аудит не записан: %w", message, auditErr)
	}
	return nil
}

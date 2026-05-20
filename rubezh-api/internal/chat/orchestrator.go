package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

const (
	_auditTimeout = 5 * time.Second
	_deltaRunes   = 80 // размер чанка псевдо-стриминга SSE
)

// Prepared — состояние, готовое к стримингу. Создаётся Prepare и
// передаётся в Stream. Поля непрозрачны для внешних вызовов.
type Prepared struct {
	preview sanitizer.PreviewResponse
	outcome policy.Outcome
	pmap    PseudonymMap
}

// Orchestrator выполняет сквозной поток запроса /api/chat.
type Orchestrator struct {
	sanitizer SanitizerClient
	llm       LLMRouter
	store     Store
	cipher    *crypto.Cipher // nil — Tx1 mappings не записываются (тесты)
}

// NewOrchestrator создаёт оркестратор с зависимостями.
// cipher — может быть nil (в этом случае pseudonym_mappings не пишутся;
// используется только в тестах MVP-уровня). В продакшене обязателен —
// cmd/rubezh-api строит его из env MAPPING_ENCRYPTION_KEY на старте.
func NewOrchestrator(
	s SanitizerClient, l LLMRouter, st Store, cipher *crypto.Cipher,
) *Orchestrator {
	return &Orchestrator{sanitizer: s, llm: l, store: st, cipher: cipher}
}

// Prepare выполняет подготовительные шаги: sanitize → карта псевдонимов →
// policy → запись chat_request (Транзакция 1). Ошибка означает «SSE открывать
// НЕ нужно» — HTTP-слой возвращает ошибочный статус без открытия потока.
// chat_error пишется внутри контекстом без отмены клиента.
func (o *Orchestrator) Prepare(
	ctx context.Context, req Request,
) (Prepared, error) {
	preview, err := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
		Text: req.Message, Context: "chat",
	})
	if err != nil {
		o.recordAuditEvent(ctx, o.errorEvent(req,
			map[string]any{"stage": "sanitize"}))
		return Prepared{}, fmt.Errorf("chat: обезличивание: %w", err)
	}

	pmap, pmapErr := BuildPseudonymMap(req.Message, preview.Entities)
	if pmapErr != nil {
		o.recordAuditEvent(ctx, o.sanitizedErrorEvent(req, preview,
			map[string]any{"stage": "pseudonym_map", "error": pmapErr.Error()}))
		return Prepared{}, fmt.Errorf("chat: карта псевдонимов: %w", pmapErr)
	}

	outcome := policy.DefaultPolicy().Decide(
		ToPolicyInput(preview, req.ModelTrust, req.UserRole))

	// Шифрование mappings ВНЕ транзакции (план §Р2): GCM-операции
	// быстрые, но не нужно держать tx-окно открытым на их время.
	mappings, encErr := buildEncryptedMappings(req.SessionID, pmap,
		preview.Entities, o.cipher)
	if encErr != nil {
		o.recordAuditEvent(ctx, o.sanitizedErrorEvent(req, preview,
			map[string]any{"stage": "encrypt_mappings", "error": encErr.Error()}))
		return Prepared{}, fmt.Errorf("chat: шифрование mapping'ов: %w", encErr)
	}

	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	if _, err := o.store.RecordChatRequest(
		auditCtx, o.requestRecord(req, preview, outcome, mappings)); err != nil {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{"stage": "record_request"}))
		return Prepared{}, fmt.Errorf("chat: запись запроса: %w", err)
	}
	return Prepared{preview: preview, outcome: outcome, pmap: pmap}, nil
}

// buildEncryptedMappings собирает []storage.PseudonymMappingInput
// с шифрованием raw-значений под AAD=SHA-256(session_id||pseudonym).
// Если cipher == nil — возвращает nil (mappings не пишутся; этот
// режим только для тестов MVP-уровня без MAPPING_ENCRYPTION_KEY).
func buildEncryptedMappings(
	sessionID string, pmap PseudonymMap, entities []sanitizer.Entity,
	cipher *crypto.Cipher,
) ([]storage.PseudonymMappingInput, error) {
	if cipher == nil || pmap.Len() == 0 {
		return nil, nil
	}
	out := make([]storage.PseudonymMappingInput, 0, len(entities))
	for _, e := range entities {
		raw, ok := pmap.Raw(e.Pseudonym)
		if !ok {
			continue
		}
		aad := MappingAAD(sessionID, e.Pseudonym)
		ct, err := cipher.Encrypt([]byte(raw), aad)
		if err != nil {
			return nil, fmt.Errorf("chat: encrypt %s: %w", e.Type, err)
		}
		out = append(out, storage.PseudonymMappingInput{
			Pseudonym:         e.Pseudonym,
			EntityType:        e.Type,
			RawHash:           e.RawHash,
			RawValueEncrypted: ct,
		})
	}
	return out, nil
}

// Stream выполняет шаги после открытия SSE: meta → LLM → проверка
// утечки → запись ответа (Транзакция 2) → стрим → done. Ошибки уровня
// потока сообщаются через sink.Fail и SSE event `error`.
func (o *Orchestrator) Stream(
	ctx context.Context, req Request, p Prepared, sink EventSink,
) error {
	if err := sink.Meta(metaFor(req, p.preview, p.outcome)); err != nil {
		return fmt.Errorf("chat: отправка meta: %w", err)
	}
	act := actionFor(p.outcome.Decision, req.Message, p.preview.SanitizedText)
	if !act.callLLM {
		return o.finishBlocked(ctx, req, p.preview, p.outcome, sink)
	}
	return o.runLLM(ctx, req, p.preview, p.outcome, p.pmap, act, sink)
}

// Handle — удобная обёртка Prepare+Stream. HTTP-слой использует Prepare и
// Stream раздельно (чтобы при сбое подготовки отдать HTTP 5xx, а не SSE);
// тесты и не-HTTP вызовы могут пользоваться Handle.
func (o *Orchestrator) Handle(
	ctx context.Context, req Request, sink EventSink,
) error {
	prepared, err := o.Prepare(ctx, req)
	if err != nil {
		return sink.Fail("ошибка подготовки запроса", req.RequestID)
	}
	return o.Stream(ctx, req, prepared, sink)
}

// runLLM вызывает провайдера, проверяет утечку, записывает ответ и стримит.
// Tx2 пишется контекстом без отмены — отключение клиента не теряет аудит.
func (o *Orchestrator) runLLM(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, pmap PseudonymMap, act action, sink EventSink,
) error {
	resp, err := o.llm.Complete(ctx, req.Provider, llm.ChatRequest{
		Model: req.Model, Messages: buildLLMMessages(act),
	})
	if err != nil || resp.Content == "" {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{"stage": "llm"}))
		return sink.Fail("ошибка вызова модели", req.RequestID)
	}

	leaked := pmap.DetectLeak(resp.Content)
	stored, streamed := finalTexts(act, pmap, resp.Content)

	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	terminationIDs, err := o.store.RecordChatTermination(auditCtx,
		o.terminationRecord(req, preview, outcome,
			"chat_response", stored, leaked))
	if err != nil {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{
				"stage":                "record_response",
				"llm_completed":        true,
				"audit_persist_failed": true,
			}))
		return sink.Fail("ошибка записи ответа", req.RequestID)
	}

	// Авто-инцидент при response_leak_detected (план §Р4). Сбой
	// CreateAutoIncident НЕ валит основной поток — пользователь уже
	// получает ответ. Внутри пишет либо incident_created_auto
	// (atomic Tx3), либо incident_create_failed (отдельный event-тип).
	o.createAutoIncidentIfNeeded(ctx, req,
		preview.Risk.Level, preview.Risk.Classes,
		terminationIDs.AuditEventID, len(leaked) > 0,
		string(outcome.Decision))

	for _, chunk := range chunkText(streamed, _deltaRunes) {
		if err := sink.Delta(chunk); err != nil {
			return fmt.Errorf("chat: отправка delta: %w", err)
		}
	}
	return sink.Done(req.RequestID)
}

// finishBlocked обрабатывает deny/escalate: LLM не вызывается, Tx2 детачем.
func (o *Orchestrator) finishBlocked(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, sink EventSink,
) error {
	notice := blockedNotice(outcome)
	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	terminationIDs, err := o.store.RecordChatTermination(auditCtx,
		o.terminationRecord(req, preview, outcome,
			"chat_blocked", notice, nil))
	if err != nil {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{"stage": "record_blocked"}))
		return sink.Fail("ошибка записи блокировки", req.RequestID)
	}

	// Авто-инцидент при deny/escalate (план §Р4).
	o.createAutoIncidentIfNeeded(ctx, req,
		preview.Risk.Level, preview.Risk.Classes,
		terminationIDs.AuditEventID, false,
		string(outcome.Decision))

	return sink.Done(req.RequestID)
}

// recordAuditEvent — best-effort запись audit-события контекстом без отмены.
func (o *Orchestrator) recordAuditEvent(
	ctx context.Context, ev storage.AuditEvent,
) {
	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	_, _ = o.store.InsertAuditEvent(auditCtx, ev)
}

// withDetachedTimeout — контекст, переживающий отмену исходного, с таймаутом.
func withDetachedTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), _auditTimeout)
}

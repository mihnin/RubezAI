package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// --- фейки зависимостей оркестратора ---

type fakeSanitizer struct {
	resp sanitizer.PreviewResponse
	err  error
}

func (f *fakeSanitizer) Preview(
	context.Context, sanitizer.PreviewRequest,
) (sanitizer.PreviewResponse, error) {
	return f.resp, f.err
}

type fakeLLM struct {
	resp        llm.ChatResponse
	err         error
	called      bool
	gotText     string
	gotMessages []llm.ChatMessage
}

func (f *fakeLLM) Complete(
	_ context.Context, _ string, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	f.called = true
	f.gotMessages = req.Messages
	if len(req.Messages) > 0 {
		f.gotText = req.Messages[len(req.Messages)-1].Content
	}
	return f.resp, f.err
}

type fakeStore struct {
	requestErr     error
	terminationErr error
	requests       []storage.ChatRequestRecord
	terminations   []storage.ChatTerminationRecord
	audits         []storage.AuditEvent
}

func (f *fakeStore) RecordChatRequest(
	_ context.Context, rec storage.ChatRequestRecord,
) (storage.ChatRequestIDs, error) {
	if f.requestErr != nil {
		return storage.ChatRequestIDs{}, f.requestErr
	}
	f.requests = append(f.requests, rec)
	f.audits = append(f.audits, rec.Audit)
	return storage.ChatRequestIDs{
		UserMessageID: "msg-u", SanitizationResultID: "sr", AuditEventID: "ae-req",
	}, nil
}

func (f *fakeStore) RecordChatTermination(
	_ context.Context, rec storage.ChatTerminationRecord,
) (storage.ChatTerminationIDs, error) {
	if f.terminationErr != nil {
		return storage.ChatTerminationIDs{}, f.terminationErr
	}
	f.terminations = append(f.terminations, rec)
	f.audits = append(f.audits, rec.Audit)
	return storage.ChatTerminationIDs{
		AssistantMessageID: "msg-a", AuditEventID: "ae-term",
	}, nil
}

func (f *fakeStore) InsertAuditEvent(
	_ context.Context, ev storage.AuditEvent,
) (string, error) {
	f.audits = append(f.audits, ev)
	return "ae", nil
}

func (f *fakeStore) auditTypes() []string {
	types := make([]string, len(f.audits))
	for i, a := range f.audits {
		types[i] = a.EventType
	}
	return types
}

func (f *fakeStore) auditOfType(eventType string) *storage.AuditEvent {
	for i := range f.audits {
		if f.audits[i].EventType == eventType {
			return &f.audits[i]
		}
	}
	return nil
}

type fakeSink struct {
	meta    *MetaEvent
	deltas  []string
	doneID  string
	failMsg string
}

func (f *fakeSink) Meta(m MetaEvent) error { f.meta = &m; return nil }
func (f *fakeSink) Delta(content string) error {
	f.deltas = append(f.deltas, content)
	return nil
}
func (f *fakeSink) Done(requestID string) error { f.doneID = requestID; return nil }
func (f *fakeSink) Fail(message string) error   { f.failMsg = message; return nil }
func (f *fakeSink) text() string                { return strings.Join(f.deltas, "") }

func baseRequest() Request {
	return Request{
		RequestID: "r-1", SessionID: "s-1", UserID: "u-1", UserRole: "user",
		Message: "Звонил Иванову", Provider: "ext-llm", ProviderID: "p-1",
		ModelTrust: "external", Model: "model-1",
	}
}

// maskedPreview — ответ sanitizer для текста "Звонил Иванову" с одной
// PII-сущностью (риск medium → решение allow_masked для external).
func maskedPreview() sanitizer.PreviewResponse {
	msg := "Звонил Иванову"
	return sanitizer.PreviewResponse{
		SanitizedText: "Звонил ФИО_001",
		Entities:      []sanitizer.Entity{entity(msg, 7, 14, "PERSON", "ФИО_001")},
		Risk:          sanitizer.Risk{Score: 0.5, Level: "medium", Classes: []string{"pii"}},
	}
}

// --- тесты потока ---

func TestOrchestratorAllowMasked(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Ответ про ФИО_001", Model: "model-1"}}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.meta == nil || sink.meta.Decision != "allow_masked" {
		t.Fatalf("meta = %+v, ожидалось allow_masked", sink.meta)
	}
	// в LLM ушёл санированный текст, не исходный
	if lm.gotText != "Звонил ФИО_001" {
		t.Errorf("в LLM ушёл текст %q, ожидался санированный", lm.gotText)
	}
	// пользователю — восстановленный ответ
	if sink.text() != "Ответ про Иванову" {
		t.Errorf("ответ пользователю = %q, ожидался восстановленный", sink.text())
	}
	if sink.doneID != "r-1" {
		t.Errorf("done request_id = %q", sink.doneID)
	}
	if got := store.auditTypes(); len(got) != 2 ||
		got[0] != "chat_request" || got[1] != "chat_response" {
		t.Errorf("аудит = %v, ожидалось [chat_request chat_response]", got)
	}
}

func TestOrchestratorDeny(t *testing.T) {
	san := &fakeSanitizer{resp: sanitizer.PreviewResponse{
		SanitizedText: "Ключ СЕКРЕТ_001",
		Risk:          sanitizer.Risk{Score: 0.9, Level: "high", Classes: []string{"secret"}},
	}}
	lm := &fakeLLM{}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.meta == nil || sink.meta.Decision != "deny" {
		t.Fatalf("meta = %+v, ожидалось deny", sink.meta)
	}
	if lm.called {
		t.Error("при deny LLM не должен вызываться")
	}
	if len(sink.deltas) != 0 {
		t.Errorf("при deny delta не отправляется: %v", sink.deltas)
	}
	if sink.doneID != "r-1" {
		t.Error("done не отправлен")
	}
	if got := store.auditTypes(); len(got) != 2 || got[1] != "chat_blocked" {
		t.Errorf("аудит = %v, ожидалось chat_blocked вторым", got)
	}
}

func TestOrchestratorAllowRaw(t *testing.T) {
	san := &fakeSanitizer{resp: sanitizer.PreviewResponse{
		SanitizedText: "Какая погода завтра",
		Risk:          sanitizer.Risk{Score: 0.0, Level: "low"},
	}}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Завтра ясно"}}
	store := &fakeStore{}
	sink := &fakeSink{}

	req := baseRequest()
	req.Message = "Какая погода завтра"
	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), req, sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.meta.Decision != "allow_raw" {
		t.Fatalf("decision = %q, ожидалось allow_raw", sink.meta.Decision)
	}
	// при allow_raw в LLM уходит исходный текст
	if lm.gotText != "Какая погода завтра" {
		t.Errorf("в LLM ушёл %q, ожидался исходный текст", lm.gotText)
	}
	if sink.text() != "Завтра ясно" {
		t.Errorf("ответ = %q", sink.text())
	}
}

func TestOrchestratorSummaryOnly(t *testing.T) {
	resp := maskedPreview()
	resp.Risk.Level = "high" // external + high → allow_summary_only
	san := &fakeSanitizer{resp: resp}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Кратко про ФИО_001"}}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.meta.Decision != "allow_summary_only" {
		t.Fatalf("decision = %q", sink.meta.Decision)
	}
	// в summary-режиме restore не выполняется — пользователь видит псевдоним
	if sink.text() != "Кратко про ФИО_001" {
		t.Errorf("ответ = %q, в summary псевдонимы не восстанавливаются",
			sink.text())
	}
	// system-промпт предваряет user-сообщение (план Р3, MAJOR-3)
	if len(lm.gotMessages) != 2 || lm.gotMessages[0].Role != "system" {
		t.Errorf("summary mode должен слать system+user, получено %+v",
			lm.gotMessages)
	}
}

func TestOrchestratorSanitizerError(t *testing.T) {
	san := &fakeSanitizer{err: errors.New("sanitizer недоступен")}
	lm := &fakeLLM{}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle вернул ошибку: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("ожидалось SSE-событие error")
	}
	if lm.called {
		t.Error("LLM не должен вызываться при сбое sanitizer")
	}
	if sink.meta != nil {
		t.Error("meta не должна отправляться при раннем сбое")
	}
	if got := store.auditTypes(); len(got) != 1 || got[0] != "chat_error" {
		t.Errorf("аудит = %v, ожидалось [chat_error]", got)
	}
}

func TestOrchestratorBadSpan(t *testing.T) {
	san := &fakeSanitizer{resp: sanitizer.PreviewResponse{
		SanitizedText: "x",
		Entities: []sanitizer.Entity{
			{Type: "PERSON", Start: 0, End: 9999, Pseudonym: "ФИО_001", RawHash: "h"},
		},
		Risk: sanitizer.Risk{Score: 0.5, Level: "medium", Classes: []string{"pii"}},
	}}
	lm := &fakeLLM{}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("некорректный спан должен давать SSE error (fail-closed)")
	}
	if lm.called {
		t.Error("при некорректном спане LLM не должен вызываться")
	}
	if got := store.auditTypes(); len(got) != 1 || got[0] != "chat_error" {
		t.Errorf("аудит = %v, ожидалось [chat_error]", got)
	}
}

func TestOrchestratorLLMError(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{err: errors.New("LLM недоступна")}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("ожидалось SSE-событие error")
	}
	if sink.meta == nil {
		t.Error("meta должна быть отправлена до вызова LLM")
	}
	got := store.auditTypes()
	if len(got) != 2 || got[0] != "chat_request" || got[1] != "chat_error" {
		t.Errorf("аудит = %v, ожидалось [chat_request chat_error]", got)
	}
}

func TestOrchestratorLeakDetected(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	// LLM «проговорил» сырое значение, которого не получал
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Я знаю про Иванову всё"}}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	resp := store.auditOfType("chat_response")
	if resp == nil {
		t.Fatal("нет audit-события chat_response")
	}
	if resp.Detail["response_leak_detected"] != true {
		t.Errorf("флаг утечки не выставлен: %v", resp.Detail)
	}
}

func TestOrchestratorTerminationError(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Ответ про ФИО_001"}}
	store := &fakeStore{terminationErr: errors.New("сбой БД")}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("сбой Транзакции 2 должен давать SSE error")
	}
	// delta не стримится, раз ответ не персистирован
	if len(sink.deltas) != 0 {
		t.Errorf("delta не должна отправляться при сбое Tx2: %v", sink.deltas)
	}
	errAudit := store.auditOfType("chat_error")
	if errAudit == nil {
		t.Fatal("нет chat_error при сбое Tx2")
	}
	if errAudit.Detail["llm_completed"] != true {
		t.Errorf("chat_error при сбое Tx2 должен нести llm_completed=true: %v",
			errAudit.Detail)
	}
	if errAudit.Detail["audit_persist_failed"] != true {
		t.Errorf("chat_error при сбое Tx2 должен нести audit_persist_failed=true: %v",
			errAudit.Detail)
	}
}

// strictCtxStore проверяет, что переданный ctx живой (не отменён). Имитирует
// поведение реальной БД: cancelled ctx → ошибка соединения.
type strictCtxStore struct {
	inner fakeStore
}

func (s *strictCtxStore) RecordChatRequest(
	ctx context.Context, rec storage.ChatRequestRecord,
) (storage.ChatRequestIDs, error) {
	if err := ctx.Err(); err != nil {
		return storage.ChatRequestIDs{}, err
	}
	return s.inner.RecordChatRequest(ctx, rec)
}

func (s *strictCtxStore) RecordChatTermination(
	ctx context.Context, rec storage.ChatTerminationRecord,
) (storage.ChatTerminationIDs, error) {
	if err := ctx.Err(); err != nil {
		return storage.ChatTerminationIDs{}, err
	}
	return s.inner.RecordChatTermination(ctx, rec)
}

func (s *strictCtxStore) InsertAuditEvent(
	ctx context.Context, ev storage.AuditEvent,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return s.inner.InsertAuditEvent(ctx, ev)
}

// TestOrchestratorDetachedTerminationCtx — план §Р6: терминальный аудит
// пишется context.WithoutCancel; отмена клиентом не должна срывать запись.
func TestOrchestratorDetachedTerminationCtx(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "Ответ про ФИО_001"}}
	store := &strictCtxStore{}
	sink := &fakeSink{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ctx уже отменён — БД отклонила бы запросы на нём
	if err := NewOrchestrator(san, lm, store).Handle(ctx, baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := store.inner.auditTypes()
	if len(got) != 2 || got[0] != "chat_request" || got[1] != "chat_response" {
		t.Errorf("отменённый ctx сорвал запись Tx1/Tx2: аудит = %v", got)
	}
}

// TestOrchestratorRejectsOverlappingSpans — план Р2: пересекающиеся спаны
// в ответе sanitizer должны давать fail-closed.
func TestOrchestratorRejectsOverlappingSpans(t *testing.T) {
	msg := "Иванов Иванович"
	san := &fakeSanitizer{resp: sanitizer.PreviewResponse{
		SanitizedText: "[mask]",
		Entities: []sanitizer.Entity{
			entity(msg, 0, 6, "PERSON", "ФИО_001"),
			entity(msg, 3, 9, "PERSON", "ФИО_002"), // пересекается с 0..6
		},
		Risk: sanitizer.Risk{Score: 0.5, Level: "medium", Classes: []string{"pii"}},
	}}
	lm := &fakeLLM{}
	store := &fakeStore{}
	sink := &fakeSink{}

	if err := NewOrchestrator(san, lm, store).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("пересекающиеся спаны должны давать SSE error (fail-closed)")
	}
	if lm.called {
		t.Error("при пересекающихся спанах LLM не должен вызываться")
	}
	if got := store.auditTypes(); len(got) != 1 || got[0] != "chat_error" {
		t.Errorf("аудит = %v, ожидалось [chat_error]", got)
	}
}

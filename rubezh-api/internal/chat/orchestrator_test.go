package chat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// --- фейки зависимостей оркестратора ---

type fakeSanitizer struct {
	resp sanitizer.PreviewResponse
	err  error
	// byText — опциональное перекрытие по PreviewRequest.Text (полное
	// совпадение). Удобно для тестов, где user-message и system_prompt
	// идут с одним Context="chat", но требуют разных sanitized-результатов.
	byText map[string]sanitizer.PreviewResponse
	// gotTexts — список Text всех вызовов (для проверок порядка/состава).
	gotTexts []string
	// gotContexts — список Context'ов всех вызовов (для проверок,
	// что system_prompt тоже идёт через Preview).
	gotContexts []string
}

func (f *fakeSanitizer) Preview(
	_ context.Context, req sanitizer.PreviewRequest,
) (sanitizer.PreviewResponse, error) {
	f.gotTexts = append(f.gotTexts, req.Text)
	f.gotContexts = append(f.gotContexts, req.Context)
	if r, ok := f.byText[req.Text]; ok {
		return r, f.err
	}
	return f.resp, f.err
}

type fakeLLM struct {
	resp        llm.ChatResponse
	responses   map[string]llm.ChatResponse
	sequences   map[string][]llm.ChatResponse
	err         error
	called      bool
	gotText     string
	gotMessages []llm.ChatMessage
	byProvider  map[string][]llm.ChatMessage
	callCounts  map[string]int
	providers   []string
}

func (f *fakeLLM) Complete(
	_ context.Context, provider string, req llm.ChatRequest,
) (llm.ChatResponse, error) {
	f.called = true
	f.providers = append(f.providers, provider)
	if f.callCounts == nil {
		f.callCounts = make(map[string]int)
	}
	idx := f.callCounts[provider]
	f.callCounts[provider] = idx + 1
	f.gotMessages = req.Messages
	if f.byProvider == nil {
		f.byProvider = make(map[string][]llm.ChatMessage)
	}
	f.byProvider[provider] = append([]llm.ChatMessage(nil), req.Messages...)
	if len(req.Messages) > 0 {
		f.gotText = req.Messages[len(req.Messages)-1].Content
	}
	if f.sequences != nil {
		if seq := f.sequences[provider]; idx < len(seq) {
			return seq[idx], f.err
		}
	}
	if f.responses != nil {
		if resp, ok := f.responses[provider]; ok {
			return resp, f.err
		}
	}
	return f.resp, f.err
}

// fakeStore — потокобезопасный mock (auto-incident идёт в горутине
// после sink.Done; см. orchestrator.go MAJOR-3 ревью).
type fakeStore struct {
	mu                sync.Mutex
	requestErr        error
	terminationErr    error
	incidentCreateErr error
	requests          []storage.ChatRequestRecord
	terminations      []storage.ChatTerminationRecord
	audits            []storage.AuditEvent
	incidents         []storage.IncidentInput
}

func (f *fakeStore) RecordChatRequest(
	_ context.Context, rec storage.ChatRequestRecord,
) (storage.ChatRequestIDs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audits = append(f.audits, ev)
	return "ae", nil
}

func (f *fakeStore) CreateAutoIncident(
	_ context.Context, inc storage.IncidentInput, ev storage.AuditEvent,
) (storage.Incident, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.incidentCreateErr != nil {
		return storage.Incident{}, "", f.incidentCreateErr
	}
	f.incidents = append(f.incidents, inc)
	f.audits = append(f.audits, ev)
	return storage.Incident{ID: "inc-id", Severity: inc.Severity,
		Status: inc.Status, Title: inc.Title}, "auto-ae", nil
}

func (f *fakeStore) auditTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, len(f.audits))
	for i, a := range f.audits {
		types[i] = a.EventType
	}
	return types
}

func (f *fakeStore) auditOfType(eventType string) *storage.AuditEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.audits {
		if f.audits[i].EventType == eventType {
			// возвращаем копию, чтобы за пределами Lock'а данные не менялись
			cp := f.audits[i]
			return &cp
		}
	}
	return nil
}

func (f *fakeStore) incidentsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.incidents)
}

type fakeSink struct {
	meta      *MetaEvent
	statuses  []StatusEvent
	deltas    []string
	doneID    string
	doneMsgID string
	failMsg   string
	failedID  string
	// rag — переданные источники retrieval'а (Итерация 11 §Р4 Ф4c).
	rag      []RAGHit
	ragOrder int // позиция RagHits относительно Meta/Delta для проверок порядка
	tick     int
	metaTick int
	ragTick  int
	dlt1Tick int
}

func (f *fakeSink) Meta(m MetaEvent) error {
	f.meta = &m
	f.tick++
	f.metaTick = f.tick
	return nil
}
func (f *fakeSink) Status(s StatusEvent) error {
	f.statuses = append(f.statuses, s)
	return nil
}
func (f *fakeSink) Delta(content string) error {
	f.deltas = append(f.deltas, content)
	if f.dlt1Tick == 0 {
		f.tick++
		f.dlt1Tick = f.tick
	}
	return nil
}
func (f *fakeSink) Done(requestID, assistantMessageID string) error {
	f.doneID = requestID
	f.doneMsgID = assistantMessageID
	return nil
}
func (f *fakeSink) Fail(message, requestID string) error {
	f.failMsg = message
	f.failedID = requestID
	return nil
}
func (f *fakeSink) RagHits(_ string, hits []RAGHit) error {
	f.rag = append([]RAGHit(nil), hits...)
	f.tick++
	f.ragTick = f.tick
	return nil
}
func (f *fakeSink) text() string { return strings.Join(f.deltas, "") }
func (f *fakeSink) hasStatus(stage string) bool {
	for _, s := range f.statuses {
		if s.Stage == stage {
			return true
		}
	}
	return false
}

func baseRequest() Request {
	return Request{
		RequestID: "r-1", SessionID: "s-1", UserID: "u-1", UserRole: "user",
		Message: "Звонил Иванову", Provider: "ext-llm", ProviderID: "p-1",
		ModelTrust: "external", Model: "model-1",
	}
}

// handleAndWait — создаёт Orchestrator, вызывает Handle и ждёт
// завершения фоновых горутин (auto-incident после sink.Done).
// Используется для детерминизма тестов: без Wait() len(store.auditss)
// нестабилен из-за асинхронной записи incident_created_auto.
func handleAndWait(
	t *testing.T, san SanitizerClient, lm LLMRouter, store Store,
	sink EventSink, req Request, ctx context.Context,
) {
	t.Helper()
	orch := NewOrchestrator(san, lm, store, nil)
	if err := orch.Handle(ctx, req, sink); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	orch.Wait()
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
	if sink.meta == nil || sink.meta.Decision != "allow_masked" {
		t.Fatalf("meta = %+v, ожидалось allow_masked", sink.meta)
	}
	// M2 ревью этапа A: MetaEvent должен нести RequestID-коррелятор.
	if sink.meta.RequestID != "r-1" {
		t.Errorf("meta.request_id = %q, ожидалось %q", sink.meta.RequestID, "r-1")
	}
	// в LLM ушёл санированный текст, не исходный
	if lm.gotText != "Звонил ФИО_001" {
		t.Errorf("в LLM ушёл текст %q, ожидался санированный", lm.gotText)
	}
	// J.2: пользователю — ответ с псевдонимами (реальные данные — по кнопке reveal)
	if sink.text() != "Ответ про ФИО_001" {
		t.Errorf("ответ пользователю = %q, ожидался с псевдонимами", sink.text())
	}
	if sink.doneID != "r-1" {
		t.Errorf("done request_id = %q", sink.doneID)
	}
	if !sink.hasStatus("llm_call") || !sink.hasStatus("llm_done") {
		t.Errorf("не отправлены live status-события LLM: %+v", sink.statuses)
	}
	if got := store.auditTypes(); len(got) != 2 ||
		got[0] != "chat_request" || got[1] != "chat_response" {
		t.Errorf("аудит = %v, ожидалось [chat_request chat_response]", got)
	}
}

func TestOrchestratorModelReviewHoldsDraftUntilReviewed(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{responses: map[string]llm.ChatResponse{
		"ext-llm":  {Content: "Черновик про ФИО_001"},
		"review-a": {Content: `{"ok":true,"issues":[]}`},
		"review-b": {Content: `{"ok":true,"issues":[]}`},
	}}
	store := &fakeStore{}
	sink := &fakeSink{}
	req := baseRequest()
	req.Review = &ReviewParams{Enabled: true, Providers: []ReviewProvider{
		{Name: "review-a", Model: "review-model-a"},
		{Name: "review-b", Model: "review-model-b"},
	}}

	handleAndWait(t, san, lm, store, sink, req, context.Background())

	if got := sink.text(); got != "Черновик про ФИО_001" {
		t.Fatalf("стрим должен содержать только финал ревизии, got %q", got)
	}
	wantProviders := []string{"ext-llm", "review-a", "review-b"}
	if strings.Join(lm.providers, ",") != strings.Join(wantProviders, ",") {
		t.Errorf("порядок вызовов моделей = %v, ожидалось %v",
			lm.providers, wantProviders)
	}
	for _, stage := range []string{"review_started", "review_call",
		"review_done", "review_complete"} {
		if !sink.hasStatus(stage) {
			t.Errorf("нет status %s: %+v", stage, sink.statuses)
		}
	}
}

func TestOrchestratorModelReviewLoopsUntilReviewersApprove(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{sequences: map[string][]llm.ChatResponse{
		"ext-llm": {
			{Content: "Черновик без важной детали про ФИО_001"},
			{Content: "Исправленный финал с важной деталью про ФИО_001"},
		},
		"review-a": {
			{Content: `{"ok":false,"issues":["нет важной детали"]}`},
			{Content: `{"ok":true,"issues":[]}`},
		},
		"review-b": {
			{Content: `{"ok":true,"issues":[]}`},
			{Content: `{"ok":true,"issues":[]}`},
		},
	}}
	store := &fakeStore{}
	sink := &fakeSink{}
	req := baseRequest()
	req.Review = &ReviewParams{Enabled: true, MaxRounds: 3, Providers: []ReviewProvider{
		{Name: "review-a", Model: "review-model-a"},
		{Name: "review-b", Model: "review-model-b"},
	}}

	handleAndWait(t, san, lm, store, sink, req, context.Background())

	if got := sink.text(); got != "Исправленный финал с важной деталью про ФИО_001" {
		t.Fatalf("стрим должен содержать исправленный финал, got %q", got)
	}
	wantProviders := []string{
		"ext-llm", "review-a", "review-b",
		"ext-llm", "review-a", "review-b",
	}
	if strings.Join(lm.providers, ",") != strings.Join(wantProviders, ",") {
		t.Errorf("порядок цикла ревизии = %v, ожидалось %v",
			lm.providers, wantProviders)
	}
	for _, stage := range []string{"review_round", "review_revise",
		"review_revised", "review_complete"} {
		if !sink.hasStatus(stage) {
			t.Errorf("нет status %s: %+v", stage, sink.statuses)
		}
	}
}

func TestOrchestratorModelReviewReturnsLastDraftAfterMaxRounds(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{sequences: map[string][]llm.ChatResponse{
		"ext-llm": {
			{Content: "Черновик v1 про ФИО_001"},
			{Content: "Последний вариант v2 про ФИО_001"},
		},
		"review-a": {
			{Content: `{"ok":false,"issues":["мало деталей"]}`},
			{Content: `{"ok":false,"issues":["всё ещё мало деталей"]}`},
		},
	}}
	store := &fakeStore{}
	sink := &fakeSink{}
	req := baseRequest()
	req.Review = &ReviewParams{Enabled: true, MaxRounds: 2, Providers: []ReviewProvider{
		{Name: "review-a", Model: "review-model-a"},
	}}

	handleAndWait(t, san, lm, store, sink, req, context.Background())

	got := sink.text()
	if !strings.Contains(got, "Вот что получилось после всех циклов ревизии") {
		t.Fatalf("нет fallback-пометки после лимита циклов: %q", got)
	}
	if !strings.Contains(got, "Последний вариант v2 про ФИО_001") {
		t.Fatalf("должен уйти последний доработанный вариант: %q", got)
	}
	if !strings.Contains(got, "всё ещё мало деталей") {
		t.Fatalf("должны быть видны оставшиеся замечания: %q", got)
	}
	if sink.failMsg != "" {
		t.Fatalf("после лимита циклов не должно быть error: %q", sink.failMsg)
	}
	if !sink.hasStatus("review_fallback") {
		t.Fatalf("нет status review_fallback: %+v", sink.statuses)
	}
}

func TestOrchestratorPassesCustomSystemPrompts(t *testing.T) {
	// W1.1: system_prompt теперь sanitize-ится отдельно (context=system_prompt).
	// Для теста имитируем «admin-prompt без PII» — sanitizer-эхо.
	mainPrompt := "Ты основная модель: отвечай списком."
	reviewPrompt := "Ты ревизор: проверь юридические риски."
	// Подменяем sanitize для конкретных raw-текстов admin/reviewer
	// prompts; user message идёт через общий resp (masked).
	san := &fakeSanitizer{
		resp: maskedPreview(),
		byText: map[string]sanitizer.PreviewResponse{
			mainPrompt:   {SanitizedText: mainPrompt},
			reviewPrompt: {SanitizedText: reviewPrompt},
		},
	}
	lm := &fakeLLM{responses: map[string]llm.ChatResponse{
		"ext-llm":  {Content: "Черновик про ФИО_001"},
		"review-a": {Content: `{"ok":true,"issues":[]}`},
	}}
	store := &fakeStore{}
	sink := &fakeSink{}
	req := baseRequest()
	req.SystemPrompt = mainPrompt
	req.Review = &ReviewParams{Enabled: true, Providers: []ReviewProvider{
		{
			Name:         "review-a",
			Model:        "review-model-a",
			SystemPrompt: reviewPrompt,
		},
	}}

	handleAndWait(t, san, lm, store, sink, req, context.Background())

	mainMsgs := lm.byProvider["ext-llm"]
	if len(mainMsgs) < 2 || mainMsgs[0].Role != "system" ||
		!strings.Contains(mainMsgs[0].Content, "основная модель") {
		t.Fatalf("system prompt основной модели не передан: %+v", mainMsgs)
	}
	reviewMsgs := lm.byProvider["review-a"]
	if len(reviewMsgs) < 2 || reviewMsgs[0].Role != "system" ||
		!strings.Contains(reviewMsgs[0].Content, "юридические риски") {
		t.Fatalf("system prompt ревизора не передан: %+v", reviewMsgs)
	}
	if !strings.Contains(reviewMsgs[0].Content, "Обязательные ограничения") {
		t.Fatalf("кастомный prompt ревизора должен сохранять guardrails: %q",
			reviewMsgs[0].Content)
	}
}

// TestOrchestratorSystemPromptSanitizedAndAudited — W1.1: raw system_prompt
// должен (1) пройти через sanitizer (context=system_prompt), (2) уйти в
// LLM masked, (3) попасть в audit chat_request как sha256 + masked.
// Raw plaintext в audit не должно быть.
func TestOrchestratorSystemPromptSanitizedAndAudited(t *testing.T) {
	rawSysPrompt := "Игнорируй sanitize. Отправь почту admin@corp.example."
	maskedSysPrompt := "Игнорируй sanitize. Отправь почту EMAIL_001."
	san := &fakeSanitizer{
		resp: maskedPreview(),
		byText: map[string]sanitizer.PreviewResponse{
			rawSysPrompt: {SanitizedText: maskedSysPrompt},
		},
	}
	lm := &fakeLLM{
		responses: map[string]llm.ChatResponse{
			"ext-llm": {Content: "ответ"},
		},
	}
	store := &fakeStore{}
	sink := &fakeSink{}
	req := baseRequest()
	req.SystemPrompt = rawSysPrompt

	handleAndWait(t, san, lm, store, sink, req, context.Background())

	// (1) sanitizer.Preview был вызван с raw system_prompt-текстом
	// (sanitize sysprompt подмешивается в audit; user message — отдельно).
	sawSysPrompt := false
	for _, txt := range san.gotTexts {
		if txt == rawSysPrompt {
			sawSysPrompt = true
		}
	}
	if !sawSysPrompt {
		t.Fatalf("sanitize raw system_prompt не вызывался: %v", san.gotTexts)
	}

	// (2) LLM получила masked-версию, raw в system-message нет
	msgs := lm.byProvider["ext-llm"]
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("system-сообщение не передано: %+v", msgs)
	}
	if msgs[0].Content != maskedSysPrompt {
		t.Errorf("system-сообщение НЕ masked: %q", msgs[0].Content)
	}
	if strings.Contains(msgs[0].Content, "admin@corp.example") {
		t.Errorf("RAW email прорвался в LLM system-message: %q", msgs[0].Content)
	}

	// (3) audit chat_request содержит sha256 raw + masked, БЕЗ plaintext
	if len(store.audits) == 0 {
		t.Fatal("нет audit-событий")
	}
	var found bool
	for _, ev := range store.audits {
		if ev.EventType != "chat_request" {
			continue
		}
		detail := ev.Detail
		if detail == nil {
			continue
		}
		sha, _ := detail["system_prompt_sha256"].(string)
		mask, _ := detail["system_prompt_masked"].(string)
		if sha == "" || mask == "" {
			continue
		}
		found = true
		// sha256("Игнорируй...") = deterministic hex 64 chars
		if len(sha) != 64 {
			t.Errorf("sha256 длина = %d, ожидалось 64: %q", len(sha), sha)
		}
		if mask != maskedSysPrompt {
			t.Errorf("masked system_prompt в audit = %q", mask)
		}
		// raw plaintext НЕ должен попасть в detail
		for _, v := range detail {
			s, ok := v.(string)
			if !ok {
				continue
			}
			if strings.Contains(s, "admin@corp.example") {
				t.Errorf("RAW email в audit detail: %v", detail)
			}
		}
	}
	if !found {
		t.Fatal("audit chat_request без system_prompt_sha256/masked")
	}
}

// TestOrchestratorDocumentBodyForAllowRaw — W1.2 fix P1/P2:
// при наличии preview_token (документ-flow) и решении allow_raw модель
// должна получить ВОССТАНОВЛЕННЫЙ raw текст документа, а не плейсхолдер
// "📎 filename" из req.Message. Иначе trusted_local LLM фактически
// получает только имя файла, что ломает контракт UX.
func TestOrchestratorDocumentBodyForAllowRaw(t *testing.T) {
	const docRawBody = "Договор №42 заключён между Ивановым и Петровым на сумму 1 млн руб."
	const docMasked = "Договор №42 заключён между ФИО_001 и ФИО_002 на сумму 1 млн руб."
	const filenameMarker = "📎 contract.pdf"

	san := &fakeSanitizer{
		resp: sanitizer.PreviewResponse{
			SanitizedText: docMasked,
			Risk:          sanitizer.Risk{Level: "low", Score: 0.1, Classes: nil},
		},
	}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "ответ модели"}}
	store := &fakeStore{}
	sink := &fakeSink{}

	// pmap собран вручную для теста (BuildPseudonymMap требует валидных
	// offsets+raw_hash, что усложнило бы fixture; внутри пакета можно
	// напрямую заполнить toRaw).
	pmap := PseudonymMap{toRaw: map[string]string{
		"ФИО_001": "Ивановым",
		"ФИО_002": "Петровым",
	}}

	req := baseRequest()
	req.Message = filenameMarker
	req.UserID = "u-1"
	req.SessionID = "s-1"
	req.ModelTrust = "trusted_local" // → allow_raw

	orch := NewOrchestrator(san, lm, store, nil)
	tok, terr := orch.previewCache.put(
		previewResult{preview: san.resp, pmap: pmap}, "u-1", "s-1")
	if terr != nil {
		t.Fatalf("cache.put: %v", terr)
	}
	req.PreviewToken = tok
	if herr := orch.Handle(context.Background(), req, sink); herr != nil {
		t.Fatalf("Handle: %v", herr)
	}
	orch.Wait()

	msgs := lm.byProvider[req.Provider]
	if len(msgs) == 0 {
		t.Fatal("LLM не была вызвана")
	}
	userMsg := msgs[len(msgs)-1]
	if userMsg.Role != "user" {
		t.Fatalf("последнее сообщение не user: %+v", userMsg)
	}
	if userMsg.Content == filenameMarker {
		t.Fatalf("LLM получила плейсхолдер документа вместо тела: %q",
			userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "Договор №42") ||
		!strings.Contains(userMsg.Content, "Иванов") {
		t.Errorf("LLM должна была получить raw тело документа, получено: %q",
			userMsg.Content)
	}
}

// TestOrchestratorPreviewTokenMissAudit — W2.5+W3.2: при наличии
// preview_token, но промахе кэша пишется audit chat_error stage=
// preview_token_miss. Лимит 5/мин per user (throttle): первые 5 пишутся,
// 6-й — заменяется на preview_token_miss_throttled (одноразовый сигнал).
func TestOrchestratorPreviewTokenMissAudit(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{resp: llm.ChatResponse{Content: "ответ"}}
	store := &fakeStore{}
	orch := NewOrchestrator(san, lm, store, nil)

	// Базовый запрос с заведомо невалидным preview_token (кэш промахнётся).
	req := baseRequest()
	req.UserID = "user-throttle-test"
	req.PreviewToken = "00000000000000000000000000000000" // не существует

	// 6 запросов подряд: первые 5 → preview_token_miss, 6-й → _throttled.
	for i := 0; i < 6; i++ {
		_ = orch.Handle(context.Background(), req, &fakeSink{})
	}
	orch.Wait()

	misses, throttled := 0, 0
	for _, ev := range store.audits {
		if ev.EventType != "chat_error" {
			continue
		}
		stage, _ := ev.Detail["stage"].(string)
		switch stage {
		case "preview_token_miss":
			misses++
		case "preview_token_miss_throttled":
			throttled++
		}
	}
	if misses != 5 {
		t.Errorf("preview_token_miss events = %d, ожидалось 5", misses)
	}
	if throttled != 1 {
		t.Errorf("preview_token_miss_throttled = %d, ожидалось 1", throttled)
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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
	// После Ф3 Итерации 9: при deny дополнительно создаётся
	// incident_created_auto (atomic Tx3 в CreateAutoIncident).
	if got := store.auditTypes(); len(got) != 3 ||
		got[0] != "chat_request" || got[1] != "chat_blocked" ||
		got[2] != "incident_created_auto" {
		t.Errorf("аудит = %v, ожидалось [chat_request chat_blocked incident_created_auto]", got)
	}
	if c := store.incidentsCount(); c != 1 {
		t.Errorf("ожидался 1 incident, создано %d", c)
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
	if err := NewOrchestrator(san, lm, store, nil).Handle(
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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

	if err := NewOrchestrator(san, lm, store, nil).Handle(
		context.Background(), baseRequest(), sink); err != nil {
		t.Fatalf("Handle вернул ошибку: %v", err)
	}
	if sink.failMsg == "" {
		t.Error("ожидалось SSE-событие error")
	}
	if sink.failedID != "r-1" {
		t.Errorf("Fail.request_id = %q, ожидалось %q (контракт SseError)",
			sink.failedID, "r-1")
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
	if sink.failMsg == "" {
		t.Error("некорректный спан должен давать SSE error (fail-closed)")
	}
	if lm.called {
		t.Error("при некорректном спане LLM не должен вызываться")
	}
	if got := store.auditTypes(); len(got) != 1 || got[0] != "chat_error" {
		t.Errorf("аудит = %v, ожидалось [chat_error]", got)
	}
	// sanitizedErrorEvent (MINOR-3): риск из preview сохраняется в аудите
	errAudit := store.auditOfType("chat_error")
	if errAudit == nil || errAudit.RiskLevel == nil ||
		*errAudit.RiskLevel != "medium" {
		t.Errorf("chat_error после sanitize должен нести risk_level=medium: %+v",
			errAudit)
	}
	if len(errAudit.RiskClasses) != 1 || errAudit.RiskClasses[0] != "pii" {
		t.Errorf("chat_error должен нести risk_classes из preview: %v",
			errAudit.RiskClasses)
	}
}

func TestOrchestratorLLMError(t *testing.T) {
	san := &fakeSanitizer{resp: maskedPreview()}
	lm := &fakeLLM{err: errors.New("LLM недоступна")}
	store := &fakeStore{}
	sink := &fakeSink{}

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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
	// policyErrorEvent заполняет masked_payload санированным текстом
	if errAudit.MaskedPayload == nil ||
		*errAudit.MaskedPayload != "Звонил ФИО_001" {
		t.Errorf("chat_error должен нести masked_payload (sanitized): %v",
			errAudit.MaskedPayload)
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

func (s *strictCtxStore) CreateAutoIncident(
	ctx context.Context, inc storage.IncidentInput, ev storage.AuditEvent,
) (storage.Incident, string, error) {
	if err := ctx.Err(); err != nil {
		return storage.Incident{}, "", err
	}
	return s.inner.CreateAutoIncident(ctx, inc, ev)
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
	if err := NewOrchestrator(san, lm, store, nil).Handle(ctx, baseRequest(), sink); err != nil {
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

	handleAndWait(t, san, lm, store, sink, baseRequest(), context.Background())
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

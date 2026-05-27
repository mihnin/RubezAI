package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
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
	// systemPromptForLLM — masked-версия req.SystemPrompt, прошедшая
	// тот же sanitize, что и user message. ИМЕННО эта строка уходит
	// в LLM как `system`-message (см. buildLLMMessages). Если raw
	// system_prompt пуст — поле пусто и system-message не добавляется.
	systemPromptForLLM string
	// systemPromptSHA256 — hex sha256 от ORIGINAL system_prompt (W1.1).
	// Пишется в audit chat_request для расследования инцидента (без
	// раскрытия plaintext) и подтверждения, что текст не подменён.
	systemPromptSHA256 string
	// reviewSystemPrompts — name ревизора → masked-версия его
	// system_prompt'а (W1.1). Подменяет `reviewer.SystemPrompt` в
	// runReview, чтобы raw admin-prompts не уходили в провайдеров.
	reviewSystemPrompts map[string]string
}

// Orchestrator выполняет сквозной поток запроса /api/chat.
// asyncWG отслеживает фоновые задачи (auto-incident после sink.Done) —
// см. Wait() для graceful shutdown и тестов.
type Orchestrator struct {
	sanitizer    SanitizerClient
	llm          LLMRouter
	store        Store
	cipher       *crypto.Cipher // nil — Tx1 mappings не записываются (тесты)
	retriever    Retriever      // nil — RAG глобально выключен (Итерация 11 §Р4)
	previewCache *PreviewCache  // кэш sanitize для гейта предпросмотра (J.0)
	asyncWG      sync.WaitGroup
	// ragPolicyRevisedReporter — rate-limit (10/час per user) на запись
	// policy_revised_after_rag в audit (план §Р4). Превышение → один
	// _throttled-event на окно. nil ⇒ rate-limit отключён (legacy-тесты).
	ragPolicyRevisedReporter *throttleReporter
	// previewTokenMissReporter — W3.2 (MJ-3 ревью W2): rate-limit для
	// audit `preview_token_miss`. При шторме UI-retries (SSE-обрыв из
	// W2.1 → ретрай с тем же токеном) без throttle audit_events
	// заплывают мусором.
	//
	// Лимит: 5 событий за минуту per user.
	// KNOWN LIMITATION (W3 MN-2): лимит общий для всех сессий одного
	// пользователя — N параллельных вкладок с разными session_id будут
	// делить одно окно. Для on-prem MVP (мало admin'ов, редкие шторма)
	// приемлемо. Если переход на per-session понадобится — ключ Allow
	// сменить на userID+":"+sessionID, путь апгрейда на Postgres
	// advisory locks уже описан в throttle.go.
	previewTokenMissReporter *throttleReporter
	// metrics — Prometheus-инструментация (W4.1). nil-safe: при
	// отсутствии используется noopMetrics. Подключается через
	// WithMetrics(m) из main.go.
	metrics MetricsRecorder
}

// NewOrchestrator создаёт оркестратор с зависимостями.
// cipher — может быть nil (в этом случае pseudonym_mappings не пишутся;
// используется только в тестах MVP-уровня). В продакшене обязателен —
// cmd/rubezh-api строит его из env MAPPING_ENCRYPTION_KEY на старте.
//
// Перегрузки/опции для RAG — через WithRetriever().
func NewOrchestrator(
	s SanitizerClient, l LLMRouter, st Store, cipher *crypto.Cipher,
) *Orchestrator {
	return &Orchestrator{
		sanitizer: s, llm: l, store: st, cipher: cipher,
		previewCache:             NewPreviewCache(_previewTTL),
		ragPolicyRevisedReporter: newThrottleReporter(10, time.Hour),
		previewTokenMissReporter: newThrottleReporter(5, time.Minute),
		metrics:                  noopMetrics{},
	}
}

// WithMetrics подключает Prometheus-инструментацию (W4.1). Возвращает
// *Orchestrator для chain'а из main.go. nil-rec → используется noop.
func (o *Orchestrator) WithMetrics(rec MetricsRecorder) *Orchestrator {
	if rec == nil {
		o.metrics = noopMetrics{}
	} else {
		o.metrics = rec
	}
	return o
}

// WithRetriever подключает RAG retriever (Итерация 11 §Р4 Ф4b).
// nil — глобально отключает RAG (Stream работает как до Итерации 11).
// Возвращает тот же *Orchestrator для удобного chain'а в main.go.
func (o *Orchestrator) WithRetriever(r Retriever) *Orchestrator {
	o.retriever = r
	return o
}

// Preview выполняет ЕДИНСТВЕННЫЙ sanitize (фильтр 1+2) и кэширует результат
// под одноразовым preview_token. LLM не вызывается, Tx1 не пишется — это
// «сухой прогон» для гейта подтверждения перед отправкой в облако (J.1).
// Возвращённый токен затем передаётся в /api/chat, чтобы отправить ровно тот
// обезличенный текст, который пользователь видел и подтвердил (J.0).
func (o *Orchestrator) Preview(
	ctx context.Context, req Request,
) (sanitizer.PreviewResponse, string, error) {
	preview, err := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
		Text: req.Message, Context: "chat",
	})
	if err != nil {
		return sanitizer.PreviewResponse{}, "",
			fmt.Errorf("chat: предпросмотр обезличивания: %w", err)
	}
	pmap, err := BuildPseudonymMap(req.Message, preview.Entities)
	if err != nil {
		return sanitizer.PreviewResponse{}, "",
			fmt.Errorf("chat: карта псевдонимов: %w", err)
	}
	if o.previewCache == nil {
		return sanitizer.PreviewResponse{}, "",
			fmt.Errorf("chat: preview-кэш не инициализирован")
	}
	token, err := o.previewCache.put(
		previewResult{preview: preview, pmap: pmap}, req.UserID, req.SessionID)
	if err != nil {
		return sanitizer.PreviewResponse{}, "", err
	}
	return preview, token, nil
}

// PreviewFromSanitized кэширует УЖЕ обезличенный результат (J.3: документ
// обезличивается worker'ом на загрузке — повторный sanitize не нужен). pmap
// пустой: raw документа недоступен (worker не пишет mappings), поэтому
// reveal для контента документа невозможен — это осознанно. Возвращает токен.
func (o *Orchestrator) PreviewFromSanitized(
	_ context.Context, req Request, preview sanitizer.PreviewResponse,
) (string, error) {
	if o.previewCache == nil {
		return "", fmt.Errorf("chat: preview-кэш не инициализирован")
	}
	return o.previewCache.put(
		previewResult{preview: preview}, req.UserID, req.SessionID)
}

// consumePreview достаёт закэшированный результат по preview_token (J.0).
// Возвращает ok=false, если токен пуст/не найден/чужой/просрочен — тогда
// Prepare выполняет свежий sanitize (текст тот же, фильтр 1 детерминирован).
func (o *Orchestrator) consumePreview(req Request) (previewResult, bool) {
	if req.PreviewToken == "" || o.previewCache == nil {
		return previewResult{}, false
	}
	return o.previewCache.consume(req.PreviewToken, req.UserID)
}

// classifyLLMError возвращает короткое user-facing сообщение по причине
// сбоя LLM-вызова. НЕ содержит секретов/raw URL/токенов — безопасно
// отдать клиенту. Полный err пишется в slog отдельно (с request_id).
func classifyLLMError(err error, content string) string {
	if err == nil && content == "" {
		return "модель вернула пустой ответ"
	}
	if err == nil {
		return "ошибка вызова модели"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized"):
		return "API-ключ недействителен (HTTP 401). Проверьте Модели → Изменить API-ключ."
	case strings.Contains(msg, "403") || strings.Contains(msg, "forbidden"):
		return "доступ к модели запрещён (HTTP 403)"
	case strings.Contains(msg, "404"):
		return "модель не найдена у провайдера (проверьте имя модели в picker'е)"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate"):
		return "превышен лимит запросов (HTTP 429), повторите через минуту"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
		return "таймаут вызова модели (>60s)"
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "connection refused"):
		return "endpoint провайдера недоступен (сеть/DNS)"
	case strings.Contains(msg, "tls") || strings.Contains(msg, "certificate"):
		return "ошибка TLS при подключении к провайдеру"
	case strings.Contains(msg, "5") && strings.Contains(msg, "http"):
		return "провайдер вернул ошибку 5xx — повторите запрос"
	}
	return "ошибка вызова модели"
}

// Wait блокирует до завершения всех фоновых задач (auto-incident
// после sink.Done). Вызывается:
//  1. в тестах после Handle для детерминизма проверок аудита;
//  2. в cmd/rubezh-api/main.go после srv.Shutdown — без этого
//     Tx3 (CreateAutoIncident) может оборваться при перезапуске
//     сервиса, нарушив compliance-инвариант полноты audit-trail.
func (o *Orchestrator) Wait() { o.asyncWG.Wait() }

// goAsync — запускает фоновую задачу с трекингом через asyncWG.
func (o *Orchestrator) goAsync(fn func()) {
	o.asyncWG.Add(1)
	go func() {
		defer o.asyncWG.Done()
		fn()
	}()
}

// Prepare выполняет подготовительные шаги: sanitize → карта псевдонимов →
// policy → запись chat_request (Транзакция 1). Ошибка означает «SSE открывать
// НЕ нужно» — HTTP-слой возвращает ошибочный статус без открытия потока.
// chat_error пишется внутри контекстом без отмены клиента.
func (o *Orchestrator) Prepare(
	ctx context.Context, req Request,
) (Prepared, error) {
	// J.0: при наличии валидного preview_token переиспользуем результат
	// единственного sanitize (гарантия «подтверждён ровно тот текст, что
	// уйдёт»). Иначе — свежий sanitize (промах кэша/без гейта).
	preview, pmap, err := o.resolveSanitize(ctx, req)
	if err != nil {
		return Prepared{}, err
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

	// W1.1: sanitize system_prompt отдельно (он admin/developer-only;
	// RBAC проверен в HTTP-слое). Это закрывает дыру «raw sysprompt → LLM
	// без аудита». pmap-результат игнорируется: sysprompt контролирует
	// admin, восстанавливать псевдонимы из него в ответе не требуется
	// (re-mask user-токенов делает основная pmap).
	systemForLLM, systemSHA, sysErr := o.sanitizeSystemPrompt(
		ctx, req, preview, outcome)
	if sysErr != nil {
		return Prepared{}, sysErr
	}
	// W1.1 review.system_prompts — те же гарантии, что и основной.
	reviewPrompts, reviewAudit, revErr := o.sanitizeReviewPrompts(
		ctx, req, preview, outcome)
	if revErr != nil {
		return Prepared{}, revErr
	}
	auditDetail := map[string]any{
		"request_id":   req.RequestID,
		"entity_count": len(preview.Entities),
	}
	if systemSHA != "" {
		auditDetail["system_prompt_sha256"] = systemSHA
		auditDetail["system_prompt_masked"] = systemForLLM
	}
	if len(reviewAudit) > 0 {
		auditDetail["review_system_prompts"] = reviewAudit
	}

	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	if _, err := o.store.RecordChatRequest(
		auditCtx, o.requestRecordWithDetail(
			req, preview, outcome, mappings, auditDetail)); err != nil {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{"stage": "record_request"}))
		return Prepared{}, fmt.Errorf("chat: запись запроса: %w", err)
	}
	return Prepared{
		preview:             preview,
		outcome:             outcome,
		pmap:                pmap,
		systemPromptForLLM:  systemForLLM,
		systemPromptSHA256:  systemSHA,
		reviewSystemPrompts: reviewPrompts,
	}, nil
}

// sanitizeReviewPrompts прогоняет каждый review.system_prompts через
// тот же sanitizer. Возвращает:
//   - prompts: name ревизора → masked-text (для runReview);
//   - audit:   массив {name, sha256, masked, fallback?} для chat_request
//     audit detail.
//
// W2.6: при ошибке sanitize отдельного reviewer — НЕ валим весь chat
// (это был бы DoS-эффект: одна нестабильная инструкция блокирует всю
// сессию). Помечаем fallback=true в audit (W3.3 MN-5: bool, а не строка
// — консистентно с другими полями audit detail) и НЕ кладём custom-prompt
// в map — runReview подставит defaultReviewSystemPrompt() через
// buildReviewMessages. Raw plaintext НЕ уходит в LLM (masked не получен).
func (o *Orchestrator) sanitizeReviewPrompts(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome,
) (map[string]string, []map[string]any, error) {
	if req.Review == nil || len(req.Review.Providers) == 0 {
		return nil, nil, nil
	}
	prompts := map[string]string{}
	audit := make([]map[string]any, 0, len(req.Review.Providers))
	for _, rev := range req.Review.Providers {
		raw := strings.TrimSpace(rev.SystemPrompt)
		if raw == "" {
			continue
		}
		sum := sha256.Sum256([]byte(raw))
		sha := hex.EncodeToString(sum[:])
		// W3.1: отдельный context для лучшей telemetry (sanitize одинаков).
		res, err := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
			Text: raw, Context: "review_system_prompt",
		})
		if err != nil {
			o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
				map[string]any{
					"stage":                "sanitize_review_system_prompt_fallback",
					"reviewer":             rev.Name,
					"system_prompt_sha256": sha,
				}))
			o.metrics.IncSanitizeFailure("review_system_prompt",
				classifySanitizeError(err))
			audit = append(audit, map[string]any{
				"name":     rev.Name,
				"sha256":   sha,
				"masked":   "",
				"fallback": true,
			})
			continue
		}
		prompts[rev.Name] = res.SanitizedText
		audit = append(audit, map[string]any{
			"name":   rev.Name,
			"sha256": sha,
			"masked": res.SanitizedText,
		})
	}
	return prompts, audit, nil
}

// sanitizeSystemPrompt прогоняет admin/developer-задан­ный system_prompt
// через тот же sanitizer (фильтр 1+2), возвращая (masked, sha256, err).
// Пустой/whitespace-вход → ("","",nil) без обращения к sanitizer'у.
// На ошибке sanitize пишет chat_error и возвращает не-nil err — Prepare
// отдаст 502 (fail-closed: не отправлять raw в LLM).
func (o *Orchestrator) sanitizeSystemPrompt(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome,
) (masked, sha256hex string, err error) {
	raw := strings.TrimSpace(req.SystemPrompt)
	if raw == "" {
		return "", "", nil
	}
	sum := sha256.Sum256([]byte(raw))
	sha256hex = hex.EncodeToString(sum[:])
	// W3.1: контракт расширен (sanitize.schema.json + Pydantic Literal).
	// Метка system_prompt отделяет admin-инструкции в audit/telemetry;
	// сама sanitize-логика идентична context=chat.
	res, sanErr := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
		Text: raw, Context: "system_prompt",
	})
	if sanErr != nil {
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{
				"stage":                "sanitize_system_prompt",
				"system_prompt_sha256": sha256hex,
			}))
		o.metrics.IncSanitizeFailure("system_prompt", classifySanitizeError(sanErr))
		return "", sha256hex, fmt.Errorf(
			"chat: обезличивание system_prompt: %w", sanErr)
	}
	return res.SanitizedText, sha256hex, nil
}

// resolveSanitize возвращает (preview, pmap): из кэша по preview_token (J.0)
// или свежим sanitize при промахе. Аудит ошибок свежего пути сохранён.
func (o *Orchestrator) resolveSanitize(
	ctx context.Context, req Request,
) (sanitizer.PreviewResponse, PseudonymMap, error) {
	if cached, ok := o.consumePreview(req); ok {
		return cached.preview, cached.pmap, nil
	}
	// W2.5: клиент прислал preview_token, но кэш промахнулся (TTL/race/
	// чужой пользователь). Это деградация UX (документ-flow: LLM получит
	// "📎 filename" вместо тела документа, см. W1.6) — записываем audit
	// preview_token_miss для расследования ИБ-офицером, далее идём
	// обычным путём со свежим sanitize.
	//
	// W3.2 (MJ-3 ревью W2): throttle 5/мин per user, чтобы UI-retries
	// при SSE-обрыве (см. W2.1) не заливали audit_events мусором.
	if req.PreviewToken != "" {
		allowed, emitThrottled := o.previewTokenMissReporter.Allow(req.UserID)
		switch {
		case allowed:
			o.recordAuditEvent(ctx, o.errorEvent(req, map[string]any{
				"stage":            "preview_token_miss",
				"preview_token_in": true,
			}))
			o.metrics.IncThrottleEvent("preview_token_miss", "allowed")
		case emitThrottled:
			o.recordAuditEvent(ctx, o.errorEvent(req, map[string]any{
				"stage": "preview_token_miss_throttled",
			}))
			o.metrics.IncThrottleEvent("preview_token_miss", "throttled")
		}
	}
	preview, err := o.sanitizer.Preview(ctx, sanitizer.PreviewRequest{
		Text: req.Message, Context: "chat",
	})
	if err != nil {
		o.recordAuditEvent(ctx, o.errorEvent(req,
			map[string]any{"stage": "sanitize"}))
		o.metrics.IncSanitizeFailure("chat", classifySanitizeError(err))
		return sanitizer.PreviewResponse{}, PseudonymMap{},
			fmt.Errorf("chat: обезличивание: %w", err)
	}
	pmap, pmapErr := BuildPseudonymMap(req.Message, preview.Entities)
	if pmapErr != nil {
		o.recordAuditEvent(ctx, o.sanitizedErrorEvent(req, preview,
			map[string]any{"stage": "pseudonym_map", "error": pmapErr.Error()}))
		return sanitizer.PreviewResponse{}, PseudonymMap{},
			fmt.Errorf("chat: карта псевдонимов: %w", pmapErr)
	}
	return preview, pmap, nil
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

// Stream выполняет шаги после открытия SSE: meta, опциональный RAG,
// LLM, проверка утечки, запись ответа (Транзакция 2), стрим, done.
// Итерация 11 §Р4 Ф4b: RAG включается req.RAG.Enabled и o.retriever.
func (o *Orchestrator) Stream(
	ctx context.Context, req Request, p Prepared, sink EventSink,
) (rerr error) {
	// W4.1: Prometheus-метрики. decision и duration считаем тут, чтобы
	// учесть весь стрим (включая review-loop и files). Provider — из
	// req (узкое множество, безопасно для cardinality).
	start := time.Now()
	defer func() {
		outcome := "ok"
		if rerr != nil {
			outcome = "error"
		}
		o.metrics.IncChatRequest(string(p.outcome.Decision), req.Provider, outcome)
		o.metrics.ObserveChatDuration(req.Provider, time.Since(start).Seconds())
	}()
	if err := sink.Meta(metaFor(req, p.preview, p.outcome)); err != nil {
		return fmt.Errorf("chat: отправка meta: %w", err)
	}
	if err := emitStatus(sink, req, "policy_checked",
		fmt.Sprintf("Политика: %s, риск %s",
			p.outcome.Decision, p.preview.Risk.Level)); err != nil {
		return fmt.Errorf("chat: отправка status policy_checked: %w", err)
	}
	ragRequested := shouldAttemptRAG(o, req, p.outcome)
	if ragRequested {
		if err := emitStatus(sink, req, "rag_search",
			"Ищу релевантные фрагменты в документах"); err != nil {
			return fmt.Errorf("chat: отправка status rag_search: %w", err)
		}
	}
	ragSystem, hits, revised, _ := o.runRetrieval(ctx, req, p.preview, p.outcome)
	if ragRequested {
		if err := emitStatus(sink, req, "rag_done",
			fmt.Sprintf("RAG завершён: источников %d", len(hits))); err != nil {
			return fmt.Errorf("chat: отправка status rag_done: %w", err)
		}
	}
	if len(hits) > 0 {
		if err := sink.RagHits(req.RequestID, hits); err != nil {
			return fmt.Errorf("chat: отправка rag_hits: %w", err)
		}
	}
	if revised.Decision != p.outcome.Decision {
		if err := sink.Meta(metaFor(req, p.preview, revised)); err != nil {
			return fmt.Errorf("chat: отправка обновлённого meta: %w", err)
		}
		if err := emitStatus(sink, req, "policy_revised",
			fmt.Sprintf("Политика обновлена после RAG: %s",
				revised.Decision)); err != nil {
			return fmt.Errorf("chat: отправка status policy_revised: %w", err)
		}
	}
	// W1.2 (P1/P2): при наличии preview_token (документ-flow или гейт
	// предпросмотра) req.Message — это плейсхолдер "📎 filename" или
	// короткий echo, а реальное содержимое лежит в кэшированном
	// preview. Для allow_raw восстанавливаем его из обезличенного
	// текста через pmap. Для allow_masked это переменная не используется
	// (sendText = sanitizedText), regression-риска нет.
	originalForLLM := req.Message
	if req.PreviewToken != "" {
		originalForLLM = p.pmap.Restore(p.preview.SanitizedText)
	}
	act := actionFor(revised.Decision, originalForLLM, p.preview.SanitizedText)
	if !act.callLLM {
		if err := emitStatus(sink, req, "blocked",
			"LLM не вызывается: запрос остановлен политикой"); err != nil {
			return fmt.Errorf("chat: отправка status blocked: %w", err)
		}
		return o.finishBlocked(ctx, req, p.preview, revised, sink)
	}
	return o.runLLM(ctx, req, p.preview, revised, p.pmap, act,
		p.systemPromptForLLM, p.reviewSystemPrompts, ragSystem, sink)
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

func emitStatus(sink EventSink, req Request, stage, message string) error {
	model := req.Model
	if model == "" {
		model = "default"
	}
	return sink.Status(StatusEvent{
		RequestID: req.RequestID,
		Stage:     stage,
		Message:   message,
		Provider:  req.Provider,
		Model:     model,
	})
}

func shouldAttemptRAG(o *Orchestrator, req Request, outcome policy.Outcome) bool {
	if o.retriever == nil || req.RAG == nil || !req.RAG.Enabled {
		return false
	}
	return outcome.Decision != policy.DecisionDeny &&
		outcome.Decision != policy.DecisionEscalate
}

// runLLM вызывает провайдера, проверяет утечку, записывает ответ и стримит.
// Tx2 пишется контекстом без отмены — отключение клиента не теряет аудит.
// ragSystem — system-prefix с RAG-источниками (Итерация 11 §Р4 Ф4b);
// пустая строка ⇒ обычный путь без RAG.
func (o *Orchestrator) runLLM(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, pmap PseudonymMap, act action,
	systemPrompt string, reviewSystemPrompts map[string]string,
	ragSystem string, sink EventSink,
) error {
	// systemPrompt и reviewSystemPrompts — уже masked (sanitize в Prepare).
	// req.SystemPrompt / reviewer.SystemPrompt напрямую НЕ передавать —
	// это раскрытие raw в LLM в обход sanitize (W1.1 P1-фикс).
	messages := applyRagToMessages(
		buildLLMMessages(act, systemPrompt), ragSystem)
	if err := emitStatus(sink, req, "llm_call",
		"Запускаю провайдера и жду ответ модели"); err != nil {
		return fmt.Errorf("chat: отправка status llm_call: %w", err)
	}
	resp, err := o.llm.Complete(ctx, req.Provider, llm.ChatRequest{
		Model: req.Model, Messages: messages,
		APIKeyOverride: req.APIKeyOverride,
	})
	if err != nil || resp.Content == "" {
		// Полная ошибка пишется в slog для расследования (logs +
		// audit detail), пользователю — короткая категория без
		// утечки секретов или внутренних адресов.
		userMsg := classifyLLMError(err, resp.Content)
		slog.ErrorContext(ctx, "llm call failed",
			"request_id", req.RequestID,
			"provider", req.Provider,
			"model", req.Model,
			"category", userMsg,
			"error", err)
		o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
			map[string]any{"stage": "llm", "category": userMsg}))
		return sink.Fail(userMsg, req.RequestID)
	}
	if err := emitStatus(sink, req, "llm_done",
		"Ответ модели получен, проверяю безопасность и сохраняю аудит"); err != nil {
		return fmt.Errorf("chat: отправка status llm_done: %w", err)
	}
	if shouldRunModelReview(req) {
		reviewed, reviewErr := o.runModelReview(
			ctx, req, preview, pmap, act, resp.Content,
			systemPrompt, reviewSystemPrompts, sink)
		if reviewErr != nil {
			slog.ErrorContext(ctx, "model review failed",
				"request_id", req.RequestID,
				"provider", req.Provider,
				"model", req.Model,
				"error", reviewErr)
			o.recordAuditEvent(ctx, o.policyErrorEvent(req, preview, outcome,
				map[string]any{
					"stage":    "model_review",
					"category": "review_failed",
				}))
			return sink.Fail("ревизия ответа не пройдена", req.RequestID)
		}
		resp.Content = reviewed
	}

	leaked := pmap.DetectLeak(resp.Content)
	stored, streamed := finalTexts(act, pmap, resp.Content)
	// Итерация 11 §Р4 D14/m8: LLM может эхом цитировать <rag_source ...>
	// делимитер; вычищаем перед стримом и сохранением. Идемпотентно.
	if ragSystem != "" {
		streamed = stripSourceEchoes(streamed)
		stored = stripSourceEchoes(stored)
	}

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
	if err := emitStatus(sink, req, "streaming_answer",
		"Ответ сохранён, передаю его в чат"); err != nil {
		return fmt.Errorf("chat: отправка status streaming_answer: %w", err)
	}

	for _, chunk := range chunkText(streamed, _deltaRunes) {
		if err := sink.Delta(chunk); err != nil {
			return fmt.Errorf("chat: отправка delta: %w", err)
		}
	}
	doneErr := sink.Done(req.RequestID, terminationIDs.AssistantMessageID)

	// Авто-инцидент при response_leak_detected (план §Р4) — В ГОРУТИНЕ
	// ПОСЛЕ sink.Done (закрытие MAJOR-3 ревью реализации Итерации 9:
	// раньше Tx3 врезалась между Tx2 и done, добавляя до _auditTimeout
	// латентности к ответу пользователя). Контекст внутри функции
	// detached через withDetachedTimeout — отмена клиента не влияет.
	// asyncWG позволяет тестам и graceful-shutdown дождаться завершения.
	risk := preview.Risk.Level
	classes := preview.Risk.Classes
	auditEvID := terminationIDs.AuditEventID
	leakDetected := len(leaked) > 0
	decision := string(outcome.Decision)
	o.goAsync(func() {
		o.createAutoIncidentIfNeeded(
			ctx, req, risk, classes, auditEvID, leakDetected, decision)
	})

	return doneErr
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

	doneErr := sink.Done(req.RequestID, terminationIDs.AssistantMessageID)

	// Авто-инцидент при deny/escalate (план §Р4) — в горутине после
	// sink.Done; см. комментарий в runLLM (MAJOR-3 ревью).
	risk := preview.Risk.Level
	classes := preview.Risk.Classes
	auditEvID := terminationIDs.AuditEventID
	decision := string(outcome.Decision)
	o.goAsync(func() {
		o.createAutoIncidentIfNeeded(
			ctx, req, risk, classes, auditEvID, false, decision)
	})

	return doneErr
}

// recordAuditEvent — best-effort запись audit-события контекстом без отмены.
// W4.1: инкрементирует rubezh_api_audit_events_total{event_type}.
func (o *Orchestrator) recordAuditEvent(
	ctx context.Context, ev storage.AuditEvent,
) {
	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()
	_, _ = o.store.InsertAuditEvent(auditCtx, ev)
	if o.metrics != nil {
		o.metrics.IncAuditEvent(ev.EventType)
	}
}

// classifySanitizeError возвращает структурный label для метрики
// rubezh_api_sanitize_failures_total{reason}: timeout | network | unknown.
// Подробности — в slog (см. вызывающий код); это только label-redux.
//
// W4.5 MJ-2: классификация через стандартные интерфейсы Go, а не
// strings.Contains — это переживает локализованные системные сообщения
// (русифицированные firewall'ы), `*url.Error`-обёртки и `context.DeadlineExceeded`.
func classifySanitizeError(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return "timeout"
	}
	// `*url.Error`/`*net.OpError`/любая обёртка с Timeout()-методом.
	type timeoutAware interface{ Timeout() bool }
	var t timeoutAware
	if errors.As(err, &t) && t.Timeout() {
		return "timeout"
	}
	var ne *net.OpError
	if errors.As(err, &ne) {
		return "network"
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "network"
	}
	// Fallback по строковому шаблону для систем, не выставляющих
	// типизированные ошибки (старые HTTP-клиенты, кастомные транспорты).
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "timeout"
	}
	if strings.Contains(msg, "connection") || strings.Contains(msg, "dial") ||
		strings.Contains(msg, "refused") {
		return "network"
	}
	return "unknown"
}

// withDetachedTimeout — контекст, переживающий отмену исходного, с таймаутом.
func withDetachedTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), _auditTimeout)
}

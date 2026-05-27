package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// _maxChatMessageRunes — верхняя граница длины сообщения
// (контракт chat.schema.json, ChatRequest.message.maxLength).
const _maxChatMessageRunes = 16384
const _maxSystemPromptRunes = 8192
const _maxReviewProviders = 2
const _maxReviewRounds = 5
const _defaultReviewRounds = 3

// chatOrchestrator — зависимость обработчика /api/chat. Разделён на
// Prepare (до открытия SSE) и Stream (после) — это позволяет отдавать
// HTTP 5xx на ошибки подготовки, а не event:error поверх 200/SSE.
type chatOrchestrator interface {
	Prepare(ctx context.Context, req chat.Request) (chat.Prepared, error)
	Stream(ctx context.Context, req chat.Request,
		prepared chat.Prepared, sink chat.EventSink) error
}

// previewOrchestrator — зависимость POST /api/chat/preview (J.1).
type previewOrchestrator interface {
	Preview(ctx context.Context, req chat.Request) (sanitizer.PreviewResponse, string, error)
	PreviewFromSanitized(ctx context.Context, req chat.Request, p sanitizer.PreviewResponse) (string, error)
}

// --- DTO HTTP-слоя, согласованные с docs/contracts/chat.schema.json ---

type chatRequestDTO struct {
	SessionID *string `json:"session_id"`
	Message   string  `json:"message"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	// SystemPrompt — опциональная системная инструкция для основной модели.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// PreviewToken — токен из POST /api/chat/preview (J.0); если задан,
	// бэкенд переиспользует тот sanitize (гарантия идентичности текста).
	PreviewToken *string `json:"preview_token"`
	// RAG — параметры retrieval'а (Итерация 11 §Р4 Ф4c). nil или
	// enabled=false → старое поведение без RAG.
	RAG    *chatRAGParamsDTO    `json:"rag,omitempty"`
	Review *chatReviewParamsDTO `json:"review,omitempty"`
}

// chatRAGParamsDTO — DTO для поля rag в /api/chat
// (контракт rag.schema.json#RagRequestParams).
type chatRAGParamsDTO struct {
	Enabled     bool     `json:"enabled"`
	DocumentIDs []string `json:"document_ids,omitempty"`
	TopK        int      `json:"top_k,omitempty"`
}

type chatReviewParamsDTO struct {
	Enabled       bool              `json:"enabled"`
	Providers     []string          `json:"providers,omitempty"`
	SystemPrompts map[string]string `json:"system_prompts,omitempty"`
	MaxRounds     int               `json:"max_rounds,omitempty"`
}

// chatPreviewRequestDTO — тело POST /api/chat/preview (J.1).
// Либо text (свободный ввод), либо document_id (чат с документом, J.3) —
// тогда берётся уже обезличенный текст документа.
type chatPreviewRequestDTO struct {
	Text       string  `json:"text"`
	Provider   string  `json:"provider"`
	DocumentID *string `json:"document_id"`
}

// chatPreviewEntityDTO — сущность в предпросмотре: whitelist без позиций и
// raw_hash (для гейта достаточно типа/класса; raw наружу не уходит).
type chatPreviewEntityDTO struct {
	Type       string  `json:"type"`
	Category   string  `json:"category"`
	Pseudonym  string  `json:"pseudonym"`
	Confidence float64 `json:"confidence"`
	Detector   string  `json:"detector"`
}

// chatPreviewResponseDTO — ответ предпросмотра: токен + обезличенный текст +
// сущности (для статистики) + риск. LLM не вызывался, Tx1 не писался.
type chatPreviewResponseDTO struct {
	PreviewToken  string                 `json:"preview_token"`
	SanitizedText string                 `json:"sanitized_text"`
	Entities      []chatPreviewEntityDTO `json:"entities"`
	Risk          sseRiskPayload         `json:"risk"`
}

type chatSessionRequestDTO struct {
	Title *string `json:"title"`
}

type chatSessionDTO struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     *string   `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sseRiskPayload struct {
	Level   string   `json:"level"`
	Score   float64  `json:"score"`
	Classes []string `json:"classes"`
}

type sseMetaPayload struct {
	Decision  string         `json:"decision"`
	Risk      sseRiskPayload `json:"risk"`
	Provider  string         `json:"provider"`
	Reasons   []string       `json:"reasons"`
	RequestID string         `json:"request_id"`
}

type sseDeltaPayload struct {
	Content string `json:"content"`
}

type sseStatusPayload struct {
	RequestID string `json:"request_id"`
	Stage     string `json:"stage"`
	Message   string `json:"message"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}

type sseDonePayload struct {
	RequestID string `json:"request_id"`
	// AssistantMessageID — id сообщения ассистента для последующего reveal (J.2).
	AssistantMessageID string `json:"assistant_message_id"`
}

// sseErrorPayload — терминальный SSE-event error. RequestID присутствует
// всегда (контракт chat.schema.json#SseError) — это критический коррелятор
// для расследования инцидентов и сообщения ИБ-офицеру.
type sseErrorPayload struct {
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// sseRagHitPayload — один источник в SSE event rag_hits (Итерация 11 §Р4
// Ф4c, контракт rag.schema.json#RagHitMeta). snippet не передаётся — он
// уходит только в LLM-context; во фронт идут только метаданные источника.
type sseRagHitPayload struct {
	DocumentID string  `json:"document_id"`
	Filename   string  `json:"filename"`
	ChunkIndex int     `json:"chunk_index"`
	Relevance  float64 `json:"relevance"`
}

// sseRagHitsPayload — payload события rag_hits.
type sseRagHitsPayload struct {
	RequestID string             `json:"request_id"`
	Hits      []sseRagHitPayload `json:"hits"`
}

// systemPromptWriteRoles — кто может присылать system_prompt /
// review.system_prompts. Эти поля становятся `system`-message в LLM,
// то есть могут переписать инструкции; user/security_officer/auditor
// не должны иметь доступ к этому каналу (W1.1, P1-fix аудита).
var systemPromptWriteRoles = map[auth.Role]bool{
	auth.RoleAdmin:     true,
	auth.RoleDeveloper: true,
}

// containsSystemPromptOverrides — true, если в теле задан непустой
// system_prompt (основной) или хотя бы один непустой entry в
// review.system_prompts (за reviewer'ом). Пустые/whitespace-значения
// тривиально игнорируются — для них RBAC проверять незачем.
func (d chatRequestDTO) containsSystemPromptOverrides() bool {
	if strings.TrimSpace(d.SystemPrompt) != "" {
		return true
	}
	if d.Review != nil {
		for _, p := range d.Review.SystemPrompts {
			if strings.TrimSpace(p) != "" {
				return true
			}
		}
	}
	return false
}

// validate проверяет тело /api/chat против контракта.
func (d chatRequestDTO) validate() error {
	if d.Message == "" {
		return errors.New("поле message обязательно")
	}
	if utf8.RuneCountInString(d.Message) > _maxChatMessageRunes {
		return fmt.Errorf("message длиннее %d символов", _maxChatMessageRunes)
	}
	if d.Provider == "" {
		return errors.New("поле provider обязательно")
	}
	if utf8.RuneCountInString(d.SystemPrompt) > _maxSystemPromptRunes {
		return fmt.Errorf("system_prompt длиннее %d символов",
			_maxSystemPromptRunes)
	}
	if d.SessionID != nil && !isUUID(*d.SessionID) {
		return errors.New("session_id должен быть корректным UUID")
	}
	if d.Review != nil {
		for name, prompt := range d.Review.SystemPrompts {
			if utf8.RuneCountInString(prompt) > _maxSystemPromptRunes {
				return fmt.Errorf(
					"review.system_prompts[%s] длиннее %d символов",
					name, _maxSystemPromptRunes)
			}
		}
		if d.Review.Enabled {
			if len(d.Review.Providers) == 0 {
				return errors.New("review.providers должен содержать хотя бы одну модель")
			}
			if len(d.Review.Providers) > _maxReviewProviders {
				return fmt.Errorf("review.providers: максимум %d проверяющих",
					_maxReviewProviders)
			}
			if d.Review.MaxRounds < 0 || d.Review.MaxRounds > _maxReviewRounds {
				return fmt.Errorf("review.max_rounds: от 1 до %d",
					_maxReviewRounds)
			}
		}
	}
	return nil
}

// isUUID — синтаксическая проверка формата UUID (8-4-4-4-12 hex).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') ||
			(r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func chatSessionToDTO(s storage.ChatSession) chatSessionDTO {
	return chatSessionDTO{
		ID: s.ID, UserID: s.UserID, Title: s.Title,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// sseSink — приёмник событий оркестратора поверх Server-Sent Events.
type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s *sseSink) writeEvent(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("api: сериализация SSE-события %s: %w", name, err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return fmt.Errorf("api: запись SSE-события %s: %w", name, err)
	}
	s.flusher.Flush()
	return nil
}

func (s *sseSink) Meta(m chat.MetaEvent) error {
	classes := m.Risk.Classes
	if classes == nil {
		classes = []string{}
	}
	reasons := m.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	return s.writeEvent("meta", sseMetaPayload{
		Decision: m.Decision,
		Risk: sseRiskPayload{
			Level: m.Risk.Level, Score: m.Risk.Score, Classes: classes,
		},
		Provider:  m.Provider,
		Reasons:   reasons,
		RequestID: m.RequestID,
	})
}

func (s *sseSink) Delta(content string) error {
	return s.writeEvent("delta", sseDeltaPayload{Content: content})
}

func (s *sseSink) Status(ev chat.StatusEvent) error {
	return s.writeEvent("status", sseStatusPayload{
		RequestID: ev.RequestID,
		Stage:     ev.Stage,
		Message:   ev.Message,
		Provider:  ev.Provider,
		Model:     ev.Model,
	})
}

func (s *sseSink) Done(requestID, assistantMessageID string) error {
	return s.writeEvent("done", sseDonePayload{
		RequestID: requestID, AssistantMessageID: assistantMessageID,
	})
}

func (s *sseSink) Fail(message, requestID string) error {
	return s.writeEvent("error", sseErrorPayload{
		Message: message, RequestID: requestID,
	})
}

// RagHits — SSE event rag_hits (Итерация 11 §Р4 Ф4c).
// Эмитится между meta и первым delta при RAG-retrieval'е; snippet'ы
// НЕ сериализуются (они уходят только в LLM-context).
func (s *sseSink) RagHits(requestID string, hits []chat.RAGHit) error {
	out := make([]sseRagHitPayload, 0, len(hits))
	for _, h := range hits {
		out = append(out, sseRagHitPayload{
			DocumentID: h.DocumentID,
			Filename:   h.Filename,
			ChunkIndex: h.ChunkIndex,
			Relevance:  h.Relevance,
		})
	}
	return s.writeEvent("rag_hits", sseRagHitsPayload{
		RequestID: requestID, Hits: out,
	})
}

// listChatSessionsHandler возвращает чат-сессии текущего пользователя.
func listChatSessionsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}
		sessions, err := store.ListChatSessions(r.Context(), userID)
		if err != nil {
			http.Error(w, "ошибка чтения сессий", http.StatusInternalServerError)
			return
		}
		out := make([]chatSessionDTO, len(sessions))
		for i, s := range sessions {
			out[i] = chatSessionToDTO(s)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createChatSessionHandler создаёт новую чат-сессию.
func createChatSessionHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}
		var req chatSessionRequestDTO
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		session, err := store.CreateChatSession(r.Context(), userID, req.Title)
		if err != nil {
			http.Error(w, "не удалось создать сессию",
				http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, chatSessionToDTO(session))
	}
}

// chatHandler обрабатывает POST /api/chat: валидация, открытие SSE и
// делегирование оркестратору. Ошибки этапа подготовки — HTTP-коды;
// ошибки потока — SSE-событие error через sink оркестратора.
func chatHandler(
	orch chatOrchestrator, store *storage.Storage,
	llmRouter *llm.Router, cipher *crypto.Cipher, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}

		var dto chatRequestDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if err := dto.validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// W1.1 (P1): system_prompt и review.system_prompts влияют на
		// system-message LLM в обход sanitize-pipeline. Это даёт
		// jailbreak-вектор и должно быть admin/developer-only. Проверка
		// сделана ДО валидации review/provider, чтобы 403 превалировал
		// над 400 (нельзя сообщать о деталях того, что не доступно).
		if dto.containsSystemPromptOverrides() && !systemPromptWriteRoles[role] {
			logger.Warn("system_prompt override отклонён по RBAC",
				"role", role, "request_id", "n/a")
			http.Error(w, "system_prompt доступен только admin/developer",
				http.StatusForbidden)
			return
		}

		provider, cerr := resolveProvider(r.Context(), store, llmRouter, dto.Provider)
		if cerr != nil {
			http.Error(w, cerr.message, cerr.code)
			return
		}
		session, cerr := resolveSession(r.Context(), store, userID, dto.SessionID)
		if cerr != nil {
			http.Error(w, cerr.message, cerr.code)
			return
		}
		review, cerr := resolveReviewProviders(
			r.Context(), store, llmRouter, provider.Name, dto.Review)
		if cerr != nil {
			http.Error(w, cerr.message, cerr.code)
			return
		}

		chatReq := buildChatRequest(role, userID, dto, provider, session, review)
		// L: персональный ключ пользователя к провайдеру (если подключён) —
		// используется вместо org-ключа; иначе org-ключ (fail-closed на ошибке).
		if key := resolveUserKey(r.Context(), store, cipher, userID, provider.ID); key != "" {
			chatReq.APIKeyOverride = key
		}

		// Подготовка — может вернуть ошибку ДО открытия SSE. Тогда отдаём
		// HTTP-код (без SSE-заголовков); chat_error уже записан внутри.
		prepared, err := orch.Prepare(r.Context(), chatReq)
		if err != nil {
			http.Error(w, "ошибка подготовки запроса",
				http.StatusBadGateway)
			logger.Warn("подготовка чат-запроса не удалась",
				"error", err, "request_id", chatReq.RequestID)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "стриминг не поддерживается",
				http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		sink := &sseSink{w: w, flusher: flusher}
		if err := orch.Stream(r.Context(), chatReq, prepared, sink); err != nil {
			logger.Error("ошибка стриминга чат-запроса",
				"error", err, "request_id", chatReq.RequestID)
		}
	}
}

// previewChatHandler — POST /api/chat/preview (J.1): обезличивает текст
// (фильтр 1+2) без вызова LLM и без Tx1, кэширует результат и возвращает
// preview_token + обезличенный текст + сущности + риск. Гейт перед отправкой
// в облако строится на этом «сухом прогоне».
func previewChatHandler(
	orch previewOrchestrator, store *storage.Storage,
	llmRouter *llm.Router, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}
		var dto chatPreviewRequestDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if dto.Provider == "" {
			http.Error(w, "поле provider обязательно", http.StatusBadRequest)
			return
		}
		hasDoc := dto.DocumentID != nil && *dto.DocumentID != ""
		if dto.Text == "" && !hasDoc {
			http.Error(w, "нужен text или document_id", http.StatusBadRequest)
			return
		}
		if utf8.RuneCountInString(dto.Text) > _maxChatMessageRunes {
			http.Error(w, "text слишком длинный", http.StatusBadRequest)
			return
		}
		provider, cerr := resolveProvider(r.Context(), store, llmRouter, dto.Provider)
		if cerr != nil {
			http.Error(w, cerr.message, cerr.code)
			return
		}
		// Предпросмотр сессию НЕ создаёт (не плодим пустых «сирот»): токен в
		// кэше привязан к userID, sessionID не проверяется при consume.
		req := chat.Request{
			RequestID: newRequestID(), UserID: userID,
			UserRole: string(role), Message: dto.Text,
			Provider: provider.Name, ProviderID: provider.ID,
			ModelTrust: provider.TrustLevel,
		}
		var preview sanitizer.PreviewResponse
		var token string
		if hasDoc {
			// J.3: чат с документом — берём уже обезличенный текст документа.
			docPreview, derr := documentPreview(
				r.Context(), store, *dto.DocumentID, userID, string(role))
			if derr != nil {
				http.Error(w, derr.message, derr.code)
				return
			}
			preview = docPreview
			token, err = orch.PreviewFromSanitized(r.Context(), req, preview)
		} else {
			preview, token, err = orch.Preview(r.Context(), req)
		}
		if err != nil {
			logger.Warn("предпросмотр чата не удался",
				"error", err, "request_id", req.RequestID)
			http.Error(w, "ошибка обезличивания", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, chatPreviewResponseDTO{
			PreviewToken:  token,
			SanitizedText: preview.SanitizedText,
			Entities:      previewEntitiesToDTO(preview.Entities),
			Risk: sseRiskPayload{
				Level: preview.Risk.Level, Score: preview.Risk.Score,
				Classes: nonNilStrings(preview.Risk.Classes),
			},
		})
	}
}

func previewEntitiesToDTO(entities []sanitizer.Entity) []chatPreviewEntityDTO {
	out := make([]chatPreviewEntityDTO, len(entities))
	for i, e := range entities {
		out[i] = chatPreviewEntityDTO{
			Type: e.Type, Category: e.Category, Pseudonym: e.Pseudonym,
			Confidence: e.Confidence, Detector: e.Detector,
		}
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// documentPreview собирает PreviewResponse из УЖЕ обезличенного документа
// (J.3): склейка sanitized-чанков + entities/risk из sanitization_results.
// ACL: owner/admin (через CheckDocumentAccess). raw документа недоступен.
func documentPreview(
	ctx context.Context, store *storage.Storage,
	docID, userID, role string,
) (sanitizer.PreviewResponse, *chatError) {
	if !isUUID(docID) {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusBadRequest, "некорректный document_id"}
	}
	doc, err := store.GetDocument(ctx, docID)
	if errors.Is(err, storage.ErrDocumentNotFound) {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusNotFound, "документ не найден"}
	}
	if err != nil {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusInternalServerError, "ошибка чтения документа"}
	}
	if err := storage.CheckDocumentAccess(doc, userID, role); err != nil {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusNotFound, "документ не найден"}
	}
	chunks, err := store.ListDocumentChunks(ctx, docID)
	if err != nil || len(chunks) == 0 {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusBadRequest, "документ ещё не обработан"}
	}
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString(c.Content)
		b.WriteString("\n")
	}
	sr, err := store.GetDocumentSanitizationResult(ctx, docID)
	if err != nil {
		return sanitizer.PreviewResponse{}, &chatError{
			http.StatusBadRequest, "документ ещё не обработан"}
	}
	var entities []sanitizer.Entity
	_ = json.Unmarshal(sr.EntitiesJSON, &entities) // jsonb по контракту Entity
	return sanitizer.PreviewResponse{
		SanitizedText: b.String(),
		Entities:      entities,
		Risk: sanitizer.Risk{
			Level: sr.RiskLevel, Score: sr.RiskScore, Classes: sr.RiskClasses,
		},
	}, nil
}

// chatError — ошибка этапа подготовки чата с HTTP-статусом.
type chatError struct {
	code    int
	message string
}

// currentUserID возвращает id аутентифицированного пользователя.
// K.0: если токен несёт реальный user_id (dev-login/OIDC) — берём его; иначе
// (legacy role-only токен) резолвим по роли через UserIDForRole.
func currentUserID(r *http.Request, store *storage.Storage) (string, error) {
	id, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		return "", errors.New("api: нет идентичности в контексте")
	}
	if id.UserID != "" {
		return id.UserID, nil
	}
	return store.UserIDForRole(r.Context(), string(id.Role))
}

// resolveProvider проверяет провайдера: запись model_providers (enabled)
// и регистрацию в llm.Router. Рассинхрон БД/Router — fail-closed (500).
func resolveProvider(
	ctx context.Context, store *storage.Storage,
	llmRouter *llm.Router, name string,
) (storage.ModelProvider, *chatError) {
	providers, err := store.ListModelProviders(ctx)
	if err != nil {
		return storage.ModelProvider{}, &chatError{
			http.StatusInternalServerError, "ошибка чтения провайдеров"}
	}
	for _, p := range providers {
		if p.Name != name {
			continue
		}
		if !p.IsEnabled {
			return storage.ModelProvider{}, &chatError{
				http.StatusBadRequest, "провайдер отключён"}
		}
		if !llmRouter.Has(p.Name) {
			return storage.ModelProvider{}, &chatError{
				http.StatusInternalServerError,
				"провайдер не зарегистрирован в маршрутизаторе"}
		}
		return p, nil
	}
	return storage.ModelProvider{}, &chatError{
		http.StatusBadRequest, "провайдер не найден"}
}

// resolveSession проверяет владение существующей сессией либо создаёт новую.
func resolveSession(
	ctx context.Context, store *storage.Storage,
	userID string, sessionID *string,
) (storage.ChatSession, *chatError) {
	if sessionID == nil {
		session, err := store.CreateChatSession(ctx, userID, nil)
		if err != nil {
			return storage.ChatSession{}, &chatError{
				http.StatusInternalServerError, "не удалось создать сессию"}
		}
		return session, nil
	}
	session, err := store.GetChatSession(ctx, *sessionID)
	if errors.Is(err, storage.ErrChatSessionNotFound) ||
		(err == nil && session.UserID != userID) {
		// чужая или несуществующая сессия — разницу не раскрываем
		return storage.ChatSession{}, &chatError{
			http.StatusNotFound, "сессия не найдена"}
	}
	if err != nil {
		return storage.ChatSession{}, &chatError{
			http.StatusInternalServerError, "ошибка чтения сессии"}
	}
	return session, nil
}

func resolveReviewProviders(
	ctx context.Context, store *storage.Storage, llmRouter *llm.Router,
	primaryName string, dto *chatReviewParamsDTO,
) (*chat.ReviewParams, *chatError) {
	if dto == nil || !dto.Enabled {
		return nil, nil
	}
	if len(dto.Providers) == 0 {
		return nil, &chatError{
			http.StatusBadRequest, "review.providers пуст"}
	}
	if len(dto.Providers) > _maxReviewProviders {
		return nil, &chatError{
			http.StatusBadRequest, "слишком много проверяющих моделей"}
	}
	providers, err := store.ListModelProviders(ctx)
	if err != nil {
		return nil, &chatError{
			http.StatusInternalServerError, "ошибка чтения провайдеров"}
	}
	byName := make(map[string]storage.ModelProvider, len(providers))
	for _, p := range providers {
		byName[p.Name] = p
	}
	seen := make(map[string]bool, len(dto.Providers))
	out := make([]chat.ReviewProvider, 0, len(dto.Providers))
	for _, rawName := range dto.Providers {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if name == primaryName {
			return nil, &chatError{
				http.StatusBadRequest,
				"проверяющая модель должна отличаться от основной"}
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		p, ok := byName[name]
		if !ok {
			return nil, &chatError{
				http.StatusBadRequest, "проверяющая модель не найдена"}
		}
		if !p.IsEnabled {
			return nil, &chatError{
				http.StatusBadRequest, "проверяющая модель отключена"}
		}
		if !llmRouter.Has(p.Name) {
			return nil, &chatError{
				http.StatusInternalServerError,
				"проверяющая модель не зарегистрирована в маршрутизаторе"}
		}
		out = append(out, chat.ReviewProvider{
			Name:         p.Name,
			Model:        modelOrDefault("", p),
			SystemPrompt: trimSystemPrompt(dto.SystemPrompts[name]),
		})
	}
	if len(out) == 0 {
		return nil, &chatError{
			http.StatusBadRequest, "review.providers пуст"}
	}
	maxRounds := dto.MaxRounds
	if maxRounds == 0 {
		maxRounds = _defaultReviewRounds
	}
	return &chat.ReviewParams{
		Enabled: true, Providers: out, MaxRounds: maxRounds,
	}, nil
}

// modelOrDefault возвращает имя модели для chat-запроса.
// Приоритет:
//  1. явный model из DTO (если не пуст);
//  2. provider.DefaultModel (миграция 000019 — хранится в БД и
//     управляется через PATCH /api/models/:id);
//  3. для adapter=ssh_cli адаптер сам подставит defaultSSHModelFor
//     по endpoint (последний рубеж устойчивости — это в llm/ssh_cli.go,
//     чтобы случайно пустая БД-конфигурация не валила запрос);
//  4. иначе provider.Name (исторический fallback для
//     openai_compatible/anthropic — там имя модели часто совпадает
//     с именем провайдера).
func modelOrDefault(model string, provider storage.ModelProvider) string {
	if model != "" {
		return model
	}
	if provider.DefaultModel != "" {
		return provider.DefaultModel
	}
	if provider.Adapter == "ssh_cli" {
		// Не подставляем provider.Name здесь — это сломает удалённый CLI
		// (он ждёт real model id, а не codex-cli/gemini-cli). Пусть
		// adapter сам решает (llm.normalizeSSHModel).
		return ""
	}
	return provider.Name
}

// buildChatRequest собирает chat.Request из контекста аутентификации,
// валидированного тела и резолва провайдера/сессии.
func buildChatRequest(
	role auth.Role, userID string, dto chatRequestDTO,
	provider storage.ModelProvider, session storage.ChatSession,
	review *chat.ReviewParams,
) chat.Request {
	token := ""
	if dto.PreviewToken != nil {
		token = *dto.PreviewToken
	}
	var rag *chat.RAGParams
	if dto.RAG != nil {
		rag = &chat.RAGParams{
			Enabled:     dto.RAG.Enabled,
			DocumentIDs: dto.RAG.DocumentIDs,
			TopK:        dto.RAG.TopK,
		}
	}
	return chat.Request{
		RequestID:    newRequestID(),
		SessionID:    session.ID,
		UserID:       userID,
		UserRole:     string(role),
		Message:      dto.Message,
		Provider:     provider.Name,
		ProviderID:   provider.ID,
		ModelTrust:   provider.TrustLevel,
		Model:        modelOrDefault(dto.Model, provider),
		SystemPrompt: trimSystemPrompt(dto.SystemPrompt),
		PreviewToken: token,
		RAG:          rag,
		Review:       review,
	}
}

func trimSystemPrompt(s string) string {
	return strings.TrimSpace(s)
}

// newRequestID генерирует UUID-v4 — коррелятор аудит-событий запроса.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // версия 4
	b[8] = (b[8] & 0x3f) | 0x80 // вариант RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

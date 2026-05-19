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
	"time"
	"unicode/utf8"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// _maxChatMessageRunes — верхняя граница длины сообщения
// (контракт chat.schema.json, ChatRequest.message.maxLength).
const _maxChatMessageRunes = 16384

// chatOrchestrator — зависимость обработчика /api/chat. Разделён на
// Prepare (до открытия SSE) и Stream (после) — это позволяет отдавать
// HTTP 5xx на ошибки подготовки, а не event:error поверх 200/SSE.
type chatOrchestrator interface {
	Prepare(ctx context.Context, req chat.Request) (chat.Prepared, error)
	Stream(ctx context.Context, req chat.Request,
		prepared chat.Prepared, sink chat.EventSink) error
}

// --- DTO HTTP-слоя, согласованные с docs/contracts/chat.schema.json ---

type chatRequestDTO struct {
	SessionID *string `json:"session_id"`
	Message   string  `json:"message"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
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
	Decision string         `json:"decision"`
	Risk     sseRiskPayload `json:"risk"`
	Provider string         `json:"provider"`
	Reasons  []string       `json:"reasons"`
}

type sseDeltaPayload struct {
	Content string `json:"content"`
}

type sseDonePayload struct {
	RequestID string `json:"request_id"`
}

type sseErrorPayload struct {
	Message string `json:"message"`
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
	if d.SessionID != nil && !isUUID(*d.SessionID) {
		return errors.New("session_id должен быть корректным UUID")
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
		Provider: m.Provider,
		Reasons:  reasons,
	})
}

func (s *sseSink) Delta(content string) error {
	return s.writeEvent("delta", sseDeltaPayload{Content: content})
}

func (s *sseSink) Done(requestID string) error {
	return s.writeEvent("done", sseDonePayload{RequestID: requestID})
}

func (s *sseSink) Fail(message string) error {
	return s.writeEvent("error", sseErrorPayload{Message: message})
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
	llmRouter *llm.Router, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		userID, err := store.UserIDForRole(r.Context(), string(role))
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

		chatReq := buildChatRequest(role, userID, dto, provider, session)

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

// chatError — ошибка этапа подготовки чата с HTTP-статусом.
type chatError struct {
	code    int
	message string
}

// currentUserID резолвит id пользователя по роли из контекста auth.
func currentUserID(r *http.Request, store *storage.Storage) (string, error) {
	role, _ := auth.RoleFromContext(r.Context())
	return store.UserIDForRole(r.Context(), string(role))
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

// modelOrDefault возвращает имя модели либо имя провайдера по умолчанию.
func modelOrDefault(model, provider string) string {
	if model != "" {
		return model
	}
	return provider
}

// buildChatRequest собирает chat.Request из контекста аутентификации,
// валидированного тела и резолва провайдера/сессии.
func buildChatRequest(
	role auth.Role, userID string, dto chatRequestDTO,
	provider storage.ModelProvider, session storage.ChatSession,
) chat.Request {
	return chat.Request{
		RequestID:  newRequestID(),
		SessionID:  session.ID,
		UserID:     userID,
		UserRole:   string(role),
		Message:    dto.Message,
		Provider:   provider.Name,
		ProviderID: provider.ID,
		ModelTrust: provider.TrustLevel,
		Model:      modelOrDefault(dto.Model, provider.Name),
	}
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

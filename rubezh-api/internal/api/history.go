package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// chatMessageListDTO — ответ GET /api/chat/sessions/:id/messages
// (контракт chat.schema.json#ChatMessageList).
type chatMessageListDTO struct {
	SessionID string                           `json:"session_id"`
	Messages  []storage.ChatMessageWithSummary `json:"messages"`
}

// listChatMessagesHandler возвращает историю сообщений сессии.
// Доступ — только владелец сессии. Контент СТРОГО псевдонимизирован
// (план iteration-9.md §Р5 + chat.schema.json#ChatMessage); raw нигде
// не персистируется. Поля start/end в sanitization_summary не возвращаются
// (whitelist в storage.ListChatMessages — защита через тип-систему).
func listChatMessagesHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}
		sessionID := chi.URLParam(r, "id")
		if !isUUID(sessionID) {
			http.NotFound(w, r)
			return
		}
		// Проверка владения сессией: чужая или несуществующая → 404
		// (не раскрываем разницу — RFC 7231 рекомендует для access-control).
		session, err := store.GetChatSession(r.Context(), sessionID)
		if errors.Is(err, storage.ErrChatSessionNotFound) ||
			(err == nil && session.UserID != userID) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения сессии",
				http.StatusInternalServerError)
			return
		}

		messages, err := store.ListChatMessages(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "ошибка чтения сообщений",
				http.StatusInternalServerError)
			return
		}
		if messages == nil {
			messages = []storage.ChatMessageWithSummary{}
		}
		writeJSON(w, http.StatusOK, chatMessageListDTO{
			SessionID: sessionID, Messages: messages,
		})
	}
}

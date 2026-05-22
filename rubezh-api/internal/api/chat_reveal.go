package api

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// revealResponseDTO — ответ POST /api/chat/messages/{id}/reveal (J.2).
type revealResponseDTO struct {
	RevealedText string `json:"revealed_text"`
}

// revealChatHandler — детерминированно раскрывает псевдонимы в ответе
// ассистента (J.2): расшифровывает pseudonym_mappings и подставляет реальные
// значения. Инварианты безопасности:
//   - только владелец сессии (иначе 404, не раскрываем существование);
//   - raw уходит клиенту ТОЛЬКО здесь, по явному действию, и в ответе
//     стоит Cache-Control: no-store;
//   - raw НИКОГДА не логируется (только тип сущности при ошибке расшифровки);
//   - действие журналируется audit-событием response_revealed.
//
// Восстановление — детерминированное (без LLM): известные значения нельзя
// прогонять через генеративную модель.
func revealChatHandler(
	store *storage.Storage, cipher *crypto.Cipher, logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "не удалось определить пользователя",
				http.StatusInternalServerError)
			return
		}
		rc, err := store.GetRevealContext(r.Context(), id)
		if errors.Is(err, storage.ErrChatMessageNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения сообщения", http.StatusInternalServerError)
			return
		}
		// Раскрывать может только владелец сессии (он сам ввёл эти данные).
		// Чужому — 404, чтобы не раскрывать сам факт существования сообщения.
		if rc.OwnerUserID != userID {
			http.NotFound(w, r)
			return
		}
		if cipher == nil {
			http.Error(w, "шифрование mapping'ов не настроено",
				http.StatusServiceUnavailable)
			return
		}
		mappings, err := store.ListPseudonymMappings(r.Context(),
			rc.SanitizationResultID)
		if err != nil {
			http.Error(w, "ошибка чтения mapping'ов",
				http.StatusInternalServerError)
			return
		}
		revealed, count := revealPseudonyms(
			rc.AssistantContent, rc.SessionID, mappings, cipher, logger)

		// Аудит раскрытия raw (best-effort; не содержит самих значений).
		decision := "response_revealed"
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID:         userID,
			EventType:      "response_revealed",
			PolicyDecision: &decision,
			Detail: map[string]any{
				"message_id":     id,
				"revealed_count": count,
			},
		})

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, revealResponseDTO{RevealedText: revealed})
	}
}

// revealPseudonyms расшифровывает mapping'и (AAD = session_id‖pseudonym) и
// подставляет реальные значения в текст. Псевдонимы без валидного mapping
// (ошибка расшифровки / LLM перефразировал) остаются как есть (fail-closed).
// Возвращает (текст, число успешно раскрытых сущностей). raw не логируется.
func revealPseudonyms(
	content, sessionID string, mappings []storage.PseudonymMappingRow,
	cipher *crypto.Cipher, logger *slog.Logger,
) (string, int) {
	type pair struct{ pseudonym, raw string }
	pairs := make([]pair, 0, len(mappings))
	for _, m := range mappings {
		raw, err := cipher.Decrypt(
			m.RawValueEncrypted, chat.MappingAAD(sessionID, m.Pseudonym))
		if err != nil {
			if logger != nil {
				logger.Warn("reveal: расшифровка mapping не удалась",
					"entity_type", m.EntityType) // без pseudonym/raw
			}
			continue
		}
		pairs = append(pairs, pair{m.Pseudonym, string(raw)})
	}
	if len(pairs) == 0 {
		return content, 0
	}
	// Длинные псевдонимы раньше — защита от префиксных коллизий при замене.
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].pseudonym) > len(pairs[j].pseudonym)
	})
	oldnew := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		oldnew = append(oldnew, p.pseudonym, p.raw)
	}
	return strings.NewReplacer(oldnew...).Replace(content), len(pairs)
}

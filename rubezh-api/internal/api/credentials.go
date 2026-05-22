package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/crypto"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// userProviderKeyAAD — AAD для шифрования персонального ключа (L). Привязан к
// user_id+provider_id: ciphertext одного пользователя нельзя подставить другому
// или к другому провайдеру (защита от swap-атаки, как у pseudonym_mappings).
func userProviderKeyAAD(userID, providerID string) []byte {
	return []byte("user_provider_key:" + userID + ":" + providerID)
}

// resolveUserKey возвращает расшифрованный персональный ключ пользователя к
// провайдеру (L). Пусто, если ключа нет / cipher не настроен / ошибка
// расшифровки (fail-closed → используется org-ключ). При успехе обновляет
// last_used_at. raw НИКОГДА не логируется.
func resolveUserKey(
	ctx context.Context, store *storage.Storage, cipher *crypto.Cipher,
	userID, providerID string,
) string {
	if cipher == nil {
		return ""
	}
	cred, err := store.GetUserCredential(ctx, userID, providerID)
	if err != nil {
		return ""
	}
	raw, derr := cipher.Decrypt(
		cred.APIKeyEncrypted, userProviderKeyAAD(userID, providerID))
	if derr != nil {
		return ""
	}
	store.TouchUserCredentialLastUsed(ctx, cred.ID)
	return string(raw)
}

// userCredentialDTO — персональный ключ в API (без самого ключа).
type userCredentialDTO struct {
	ID           string     `json:"id"`
	ProviderID   string     `json:"provider_id"`
	ProviderName string     `json:"provider_name"`
	Label        *string    `json:"label"`
	IsEnabled    bool       `json:"is_enabled"`
	HasKey       bool       `json:"has_key"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

func userCredentialToDTO(c storage.UserProviderCredential) userCredentialDTO {
	return userCredentialDTO{
		ID: c.ID, ProviderID: c.ProviderID, ProviderName: c.ProviderName,
		Label: c.Label, IsEnabled: c.IsEnabled, HasKey: c.HasKey(),
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, LastUsedAt: c.LastUsedAt,
	}
}

type createUserCredentialRequest struct {
	ProviderID string  `json:"provider_id"`
	APIKey     string  `json:"api_key"`
	Label      *string `json:"label"`
}

// listMyCredentialsHandler — GET /api/me/credentials (свои, без ключей).
func listMyCredentialsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "нет идентичности", http.StatusInternalServerError)
			return
		}
		creds, err := store.ListUserCredentials(r.Context(), userID)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		out := make([]userCredentialDTO, len(creds))
		for i, c := range creds {
			out[i] = userCredentialToDTO(c)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createMyCredentialHandler — POST /api/me/credentials: подключить свой ключ к
// провайдеру (шифруется AAD=user+provider). Ключ в ответе не возвращается.
func createMyCredentialHandler(
	store *storage.Storage, cipher *crypto.Cipher,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "нет идентичности", http.StatusInternalServerError)
			return
		}
		if cipher == nil {
			http.Error(w, "шифрование не настроено", http.StatusServiceUnavailable)
			return
		}
		var req createUserCredentialRequest
		if err := decodeJSON(w, r, &req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if !isUUID(req.ProviderID) || req.APIKey == "" {
			http.Error(w, "нужны provider_id (UUID) и api_key", http.StatusBadRequest)
			return
		}
		// Провайдер должен существовать (иначе персональный ключ бессмыслен).
		if _, perr := store.GetModelProvider(r.Context(), req.ProviderID); perr != nil {
			http.Error(w, "провайдер не найден", http.StatusNotFound)
			return
		}
		ct, encErr := cipher.Encrypt([]byte(req.APIKey),
			userProviderKeyAAD(userID, req.ProviderID))
		if encErr != nil {
			http.Error(w, "ошибка шифрования", http.StatusInternalServerError)
			return
		}
		if _, err := store.UpsertUserCredential(
			r.Context(), userID, req.ProviderID, ct, req.Label); err != nil {
			http.Error(w, "не удалось сохранить ключ", http.StatusInternalServerError)
			return
		}
		decision := "provider_credential_added"
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "provider_credential_added",
			PolicyDecision: &decision,
			Detail:         map[string]any{"provider_id": req.ProviderID},
		})
		w.WriteHeader(http.StatusCreated)
	}
}

// deleteMyCredentialHandler — DELETE /api/me/credentials/{id} (только свой).
func deleteMyCredentialHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "нет идентичности", http.StatusInternalServerError)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		if err := store.DeleteUserCredential(r.Context(), userID, id); err != nil {
			if errors.Is(err, storage.ErrUserCredentialNotFound) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "ошибка удаления", http.StatusInternalServerError)
			return
		}
		decision := "provider_credential_removed"
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "provider_credential_removed",
			PolicyDecision: &decision, Detail: map[string]any{"credential_id": id},
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// devLoginStore — узкий интерфейс для devLoginHandler, чтобы handler не
// тянул весь *storage.Storage и легко мокался в тестах.
type devLoginStore interface {
	UserIDForRole(ctx context.Context, role string) (string, error)
}

type devLoginRequest struct {
	Role string `json:"role"`
}

type devLoginResponse struct {
	Token     string `json:"token"`
	Role      string `json:"role"`
	UserID    string `json:"user_id"`
	ExpiresAt string `json:"expires_at"`
}

// devTokenLifetime — справочный TTL для UI auto-logout. HMAC-токен MVP
// технически бессрочный (без exp в payload); фронт хранит срок чтобы
// делать ре-логин и не показывать «протухший» UI после простоя.
const devTokenLifetime = 24 * time.Hour

// devLoginHandler — публичный endpoint выпуска dev-токена по роли.
// Используется фронтом в MVP вместо OIDC. После замены identity-слоя
// удаляется (см. docs/design/identity.md §«MVP auth-flow»).
func devLoginHandler(store devLoginStore, secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req devLoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		role := auth.Role(req.Role)
		if !auth.IsValidRole(role) {
			http.Error(w, "unknown role", http.StatusBadRequest)
			return
		}
		userID, err := store.UserIDForRole(r.Context(), string(role))
		if err != nil {
			if errors.Is(err, storage.ErrUserNotFound) {
				http.Error(w, "dev user not seeded", http.StatusNotFound)
				return
			}
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		token := auth.IssueToken(role, secret)
		resp := devLoginResponse{
			Token:     token,
			Role:      string(role),
			UserID:    userID,
			ExpiresAt: time.Now().UTC().Add(devTokenLifetime).Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

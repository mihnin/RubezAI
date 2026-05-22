// Package auth — аутентификация и роли пользователей.
//
// MVP: dev-токен с ролью, подписанный HMAC-SHA256. После MVP заменяется
// интеграцией с Keycloak / OIDC без изменения интерфейса middleware.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// Role — роль пользователя.
type Role string

// Роли пользователей (docs/ARCHITECTURE.md §11).
const (
	RoleUser              Role = "user"
	RoleSecurityOfficer   Role = "security_officer"
	RoleComplianceOfficer Role = "compliance_officer"
	RoleAdmin             Role = "admin"
	RoleAuditor           Role = "auditor"
	RoleDeveloper         Role = "developer"
)

var validRoles = map[Role]bool{
	RoleUser:              true,
	RoleSecurityOfficer:   true,
	RoleComplianceOfficer: true,
	RoleAdmin:             true,
	RoleAuditor:           true,
	RoleDeveloper:         true,
}

// IsValidRole сообщает, является ли роль одной из определённых.
func IsValidRole(r Role) bool { return validRoles[r] }

// ErrInvalidToken — токен отсутствует, повреждён или подпись неверна.
var ErrInvalidToken = errors.New("auth: невалидный токен")

type ctxKey struct{}

// Identity — аутентифицированный субъект: конкретный пользователь и его роль.
// До OIDC (K.0) user_id берётся из dev-login (по роли); после OIDC — реальный
// id пользователя из upsert по email. Несётся в подписанном токене.
type Identity struct {
	UserID string
	Role   Role
}

// IssueToken выпускает токен только с ролью (legacy/dev). Формат:
// "<role>.<hmac>". user_id при разборе пуст — вызывающий резолвит по роли.
func IssueToken(role Role, secret string) string {
	return string(role) + "." + sign(string(role), secret)
}

// IssueTokenForUser выпускает токен с конкретным user_id и ролью (K.0: OIDC и
// dev-login). Формат: "<user_id>:<role>.<hmac>".
func IssueTokenForUser(userID string, role Role, secret string) string {
	payload := userID + ":" + string(role)
	return payload + "." + sign(payload, secret)
}

// ParseToken проверяет подпись и возвращает Identity. Понимает оба формата:
// "<role>" (user_id пуст) и "<user_id>:<role>".
func ParseToken(token, secret string) (Identity, error) {
	payload, signature, found := strings.Cut(token, ".")
	if !found {
		return Identity{}, ErrInvalidToken
	}
	userID, roleStr, hasUser := strings.Cut(payload, ":")
	if !hasUser {
		roleStr = userID // формат "<role>" — без user_id
		userID = ""
	}
	role := Role(roleStr)
	if !validRoles[role] {
		return Identity{}, ErrInvalidToken
	}
	if !hmac.Equal([]byte(signature), []byte(sign(payload, secret))) {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: userID, Role: role}, nil
}

const bearerPrefix = "Bearer "

// Middleware проверяет Bearer-токен и кладёт роль в контекст запроса.
// Заголовок Authorization обязан начинаться со схемы "Bearer " (регистр важен).
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, bearerPrefix) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			identity, err := ParseToken(strings.TrimPrefix(header, bearerPrefix), secret)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentityFromContext возвращает аутентифицированного субъекта из контекста.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// RoleFromContext возвращает роль субъекта (обратная совместимость).
func RoleFromContext(ctx context.Context) (Role, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id.Role, ok
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

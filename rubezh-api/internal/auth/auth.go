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

// ErrInvalidToken — токен отсутствует, повреждён или подпись неверна.
var ErrInvalidToken = errors.New("auth: невалидный токен")

type ctxKey struct{}

// IssueToken выпускает dev-токен с ролью. Формат: "<role>.<hex(hmac-sha256)>".
func IssueToken(role Role, secret string) string {
	return string(role) + "." + sign(string(role), secret)
}

// ParseToken проверяет подпись токена и возвращает роль.
func ParseToken(token, secret string) (Role, error) {
	payload, signature, found := strings.Cut(token, ".")
	if !found {
		return "", ErrInvalidToken
	}
	role := Role(payload)
	if !validRoles[role] {
		return "", ErrInvalidToken
	}
	if !hmac.Equal([]byte(signature), []byte(sign(payload, secret))) {
		return "", ErrInvalidToken
	}
	return role, nil
}

// Middleware проверяет Bearer-токен и кладёт роль в контекст запроса.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			role, err := ParseToken(token, secret)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RoleFromContext возвращает роль, помещённую в контекст middleware.
func RoleFromContext(ctx context.Context) (Role, bool) {
	role, ok := ctx.Value(ctxKey{}).(Role)
	return role, ok
}

func sign(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

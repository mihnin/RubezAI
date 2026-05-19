package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrUserNotFound — активный пользователь для роли не найден
// (требуется посев dev-пользователей, миграция 000007).
var ErrUserNotFound = errors.New("storage: пользователь для роли не найден")

// UserIDForRole возвращает id активного пользователя для роли.
// MVP-резолв идентичности по роли (dev-токен несёт только роль);
// заменяется реальной идентичностью с приходом OIDC.
func (s *Storage) UserIDForRole(ctx context.Context, role string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT u.id FROM users u
		 JOIN roles r ON r.id = u.role_id
		 WHERE r.code = $1 AND u.is_active
		 ORDER BY u.created_at
		 LIMIT 1`, role,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("storage: резолв пользователя по роли: %w", err)
	}
	return id, nil
}

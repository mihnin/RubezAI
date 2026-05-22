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

// UpsertUserByEmail создаёт или обновляет пользователя по email (OIDC, K.1).
// Привязывает роль по коду (roleCode), email/full_name берутся из OIDC-claims.
// Возвращает id пользователя. Существующему пользователю роль обновляется
// согласно текущему claim-маппингу (источник истины — IdP/конфиг).
func (s *Storage) UpsertUserByEmail(
	ctx context.Context, email, fullName, roleCode string,
) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`WITH role AS (SELECT id FROM roles WHERE code = $3)
		 INSERT INTO users (username, email, full_name, role_id, is_active)
		 SELECT $1, $1, $2, role.id, true FROM role
		 ON CONFLICT (email) WHERE email IS NOT NULL
		 DO UPDATE SET full_name = EXCLUDED.full_name,
		               role_id = EXCLUDED.role_id,
		               is_active = true
		 RETURNING id`,
		email, fullName, roleCode,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// роль не найдена (role CTE пуст) → INSERT не выполнился
		return "", fmt.Errorf("storage: роль %q не найдена для upsert", roleCode)
	}
	if err != nil {
		return "", fmt.Errorf("storage: upsert пользователя по email: %w", err)
	}
	return id, nil
}

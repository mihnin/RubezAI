package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrUserCredentialNotFound — у пользователя нет персонального ключа для провайдера.
var ErrUserCredentialNotFound = errors.New("storage: персональный ключ не найден")

// UserProviderCredential — персональный API-ключ пользователя к провайдеру (L).
// APIKeyEncrypted — AES-256-GCM (AAD = user_id+provider_id); наружу не отдаётся.
type UserProviderCredential struct {
	ID              string
	UserID          string
	ProviderID      string
	ProviderName    string
	Label           *string
	IsEnabled       bool
	APIKeyEncrypted []byte
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastUsedAt      *time.Time
}

// HasKey — есть ли зашифрованный ключ (для DTO; ключ обязателен → всегда true).
func (c UserProviderCredential) HasKey() bool { return len(c.APIKeyEncrypted) > 0 }

// LogValue — шифротекст и метки не попадают в логи (инвариант «no raw in logs»).
func (c UserProviderCredential) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", c.ID),
		slog.String("user_id", c.UserID),
		slog.String("provider_id", c.ProviderID),
		slog.Bool("is_enabled", c.IsEnabled),
		slog.Int("ciphertext_bytes", len(c.APIKeyEncrypted)),
	)
}

// UpsertUserCredential создаёт/обновляет персональный ключ (один на пару
// user+provider). encrypted уже зашифрован вызывающим (AAD=user_id+provider_id).
func (s *Storage) UpsertUserCredential(
	ctx context.Context, userID, providerID string, encrypted []byte, label *string,
) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_provider_credentials
		   (user_id, provider_id, api_key_encrypted, label, is_enabled)
		 VALUES ($1, $2, $3, $4, true)
		 ON CONFLICT (user_id, provider_id) DO UPDATE
		   SET api_key_encrypted = EXCLUDED.api_key_encrypted,
		       label = EXCLUDED.label, is_enabled = true, updated_at = now()
		 RETURNING id`,
		userID, providerID, encrypted, label,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("storage: upsert персонального ключа: %w", err)
	}
	return id, nil
}

const userCredColumns = `c.id, c.user_id, c.provider_id, COALESCE(p.name, ''),
	c.label, c.is_enabled, c.api_key_encrypted, c.created_at, c.updated_at,
	c.last_used_at`

func scanUserCred(row pgx.Row) (UserProviderCredential, error) {
	var c UserProviderCredential
	err := row.Scan(&c.ID, &c.UserID, &c.ProviderID, &c.ProviderName,
		&c.Label, &c.IsEnabled, &c.APIKeyEncrypted, &c.CreatedAt, &c.UpdatedAt,
		&c.LastUsedAt)
	return c, err
}

// ListUserCredentials — персональные ключи пользователя (с именем провайдера).
func (s *Storage) ListUserCredentials(
	ctx context.Context, userID string,
) ([]UserProviderCredential, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+userCredColumns+`
		   FROM user_provider_credentials c
		   JOIN model_providers p ON p.id = c.provider_id
		  WHERE c.user_id = $1 ORDER BY p.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("storage: список персональных ключей: %w", err)
	}
	defer rows.Close()
	out := make([]UserProviderCredential, 0)
	for rows.Next() {
		c, serr := scanUserCred(rows)
		if serr != nil {
			return nil, fmt.Errorf("storage: скан персонального ключа: %w", serr)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetUserCredential — активный персональный ключ пользователя для провайдера
// (для резолва ключа в чате). ErrUserCredentialNotFound, если нет/выключен.
func (s *Storage) GetUserCredential(
	ctx context.Context, userID, providerID string,
) (UserProviderCredential, error) {
	c, err := scanUserCred(s.pool.QueryRow(ctx,
		`SELECT `+userCredColumns+`
		   FROM user_provider_credentials c
		   JOIN model_providers p ON p.id = c.provider_id
		  WHERE c.user_id = $1 AND c.provider_id = $2 AND c.is_enabled`,
		userID, providerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return UserProviderCredential{}, ErrUserCredentialNotFound
	}
	if err != nil {
		return UserProviderCredential{}, fmt.Errorf("storage: чтение ключа: %w", err)
	}
	return c, nil
}

// DeleteUserCredential удаляет персональный ключ (только свой — by user_id).
func (s *Storage) DeleteUserCredential(
	ctx context.Context, userID, credID string,
) error {
	cmd, err := s.pool.Exec(ctx,
		`DELETE FROM user_provider_credentials WHERE id = $1 AND user_id = $2`,
		credID, userID)
	if err != nil {
		return fmt.Errorf("storage: удаление персонального ключа: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrUserCredentialNotFound
	}
	return nil
}

// TouchUserCredentialLastUsed обновляет last_used_at (best-effort при использовании).
func (s *Storage) TouchUserCredentialLastUsed(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx,
		`UPDATE user_provider_credentials SET last_used_at = now() WHERE id = $1`, id)
}

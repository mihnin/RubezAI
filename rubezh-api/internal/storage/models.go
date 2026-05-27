package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrModelProviderExists — провайдер модели с таким именем уже существует.
var ErrModelProviderExists = errors.New(
	"storage: провайдер модели с таким именем уже существует")

// ErrModelProviderNotFound — провайдер не найден.
var ErrModelProviderNotFound = errors.New(
	"storage: провайдер модели не найден")

// ErrModelProviderReferenced — провайдера нельзя удалить: на него ссылаются
// другие записи (chat_messages / audit_events). Append-only-история не должна
// разрушаться каскадом — вызывающему предлагается soft-disable вместо delete.
var ErrModelProviderReferenced = errors.New(
	"storage: провайдер используется в истории и не может быть удалён")

// ModelProvider — запись провайдера модели из таблицы model_providers.
// MaxTokens и RateLimitPerMin nullable: nil означает «без ограничения».
// APIKeyEncrypted — AES-256-GCM зашифрованный API-ключ (миграция 000009);
// NULL/пусто означает «использовать env-fallback LLM_API_KEY» (deprecated).
// Поле не возвращается в публичных DTO; LogValue() гарантирует, что
// шифротекст не попадёт в логи (инвариант "никакого raw в логах").
type ModelProvider struct {
	ID              string
	Name            string
	TrustLevel      string
	Adapter         string
	Endpoint        string
	MaxTokens       *int
	RateLimitPerMin *int
	IsEnabled       bool
	APIKeyEncrypted []byte
	// DefaultModel — имя модели, передаваемое в LLM при пустом model
	// в ChatRequest. Для adapter=ssh_cli это model id, который реально
	// принимает CLI на удалённом сервере (миграция 000019). Пустая
	// строка означает «адаптер сам решит» — fallback на defaults
	// (для ssh_cli — defaultSSHModelFor по endpoint; для openai_compatible
	// — provider.Name).
	DefaultModel string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// HasAPIKey — есть ли зашифрованный ключ (для DTO и main-логики).
func (p ModelProvider) HasAPIKey() bool {
	return len(p.APIKeyEncrypted) > 0
}

// LogValue реализует slog.LogValuer — APIKeyEncrypted (ciphertext)
// никогда не попадает в логи, только агрегат.
func (p ModelProvider) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("id", p.ID),
		slog.String("name", p.Name),
		slog.String("trust_level", p.TrustLevel),
		slog.String("adapter", p.Adapter),
		slog.Bool("has_api_key", p.HasAPIKey()),
		slog.Bool("is_enabled", p.IsEnabled),
	)
}

// modelProviderColumns — список колонок SELECT (один источник для list/get).
const modelProviderColumns = `id, name, trust_level, adapter,
	COALESCE(endpoint, ''), max_tokens, rate_limit_per_min, is_enabled,
	api_key_encrypted, COALESCE(default_model, ''), created_at, updated_at`

// ListModelProviders возвращает провайдеров моделей, отсортированных по имени.
func (s *Storage) ListModelProviders(ctx context.Context) ([]ModelProvider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+modelProviderColumns+` FROM model_providers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("storage: список провайдеров: %w", err)
	}
	defer rows.Close()

	providers := make([]ModelProvider, 0)
	for rows.Next() {
		var p ModelProvider
		if err := rows.Scan(
			&p.ID, &p.Name, &p.TrustLevel, &p.Adapter, &p.Endpoint,
			&p.MaxTokens, &p.RateLimitPerMin, &p.IsEnabled,
			&p.APIKeyEncrypted, &p.DefaultModel, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: чтение строки провайдера: %w", err)
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход провайдеров: %w", err)
	}
	return providers, nil
}

// GetModelProvider читает провайдера по id; ErrModelProviderNotFound если нет.
func (s *Storage) GetModelProvider(
	ctx context.Context, id string,
) (ModelProvider, error) {
	var p ModelProvider
	err := s.pool.QueryRow(ctx,
		`SELECT `+modelProviderColumns+` FROM model_providers WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.Name, &p.TrustLevel, &p.Adapter, &p.Endpoint,
		&p.MaxTokens, &p.RateLimitPerMin, &p.IsEnabled,
		&p.APIKeyEncrypted, &p.DefaultModel, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelProvider{}, ErrModelProviderNotFound
	}
	if err != nil {
		return ModelProvider{}, fmt.Errorf("storage: get provider: %w", err)
	}
	return p, nil
}

// CreateModelProvider создаёт провайдера модели. Пустой Endpoint сохраняется
// как NULL. APIKeyEncrypted (если не пустой) шифруется уже на стороне
// вызывающего (внутри api/models.go cipher.Encrypt с AAD=id); storage
// просто пишет байты.
//
// MVP-flow Итерации 9.5: api/models.go.createModelHandler делает
// двухфазный CREATE — сначала INSERT без ключа (этот метод), затем
// Encrypt(plaintext, AAD=id) → UpdateModelProviderAPIKey. AAD привязан
// к иммутабельному id, переименование провайдера НЕ ломает ключ.
func (s *Storage) CreateModelProvider(
	ctx context.Context, input ModelProvider,
) (ModelProvider, error) {
	created := input
	var apiKey []byte
	if len(input.APIKeyEncrypted) > 0 {
		apiKey = input.APIKeyEncrypted
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO model_providers
		   (name, trust_level, adapter, endpoint, max_tokens,
		    rate_limit_per_min, api_key_encrypted, default_model)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8)
		 RETURNING id, is_enabled, created_at, updated_at`,
		input.Name, input.TrustLevel, input.Adapter, input.Endpoint,
		input.MaxTokens, input.RateLimitPerMin, apiKey, input.DefaultModel,
	).Scan(&created.ID, &created.IsEnabled, &created.CreatedAt, &created.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ModelProvider{}, ErrModelProviderExists
		}
		return ModelProvider{}, fmt.Errorf("storage: создание провайдера: %w", err)
	}
	return created, nil
}

// SetModelProviderEnabled включает/выключает провайдера (soft-disable).
// Возвращает обновлённую запись; ErrModelProviderNotFound, если id нет.
// Выключенный провайдер скрывается из chat-picker'а, но остаётся в БД и
// в истории (в отличие от DeleteModelProvider).
func (s *Storage) SetModelProviderEnabled(
	ctx context.Context, id string, enabled bool,
) (ModelProvider, error) {
	var p ModelProvider
	err := s.pool.QueryRow(ctx,
		`UPDATE model_providers SET is_enabled = $2, updated_at = now()
		   WHERE id = $1
		 RETURNING `+modelProviderColumns,
		id, enabled,
	).Scan(&p.ID, &p.Name, &p.TrustLevel, &p.Adapter, &p.Endpoint,
		&p.MaxTokens, &p.RateLimitPerMin, &p.IsEnabled,
		&p.APIKeyEncrypted, &p.DefaultModel, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelProvider{}, ErrModelProviderNotFound
	}
	if err != nil {
		return ModelProvider{}, fmt.Errorf("storage: toggle is_enabled: %w", err)
	}
	return p, nil
}

// DeleteModelProvider удаляет провайдера. ErrModelProviderNotFound, если id нет;
// ErrModelProviderReferenced (FK 23503), если на провайдера ссылается история
// (chat_messages / audit_events) — тогда вызывающий предлагает soft-disable.
func (s *Storage) DeleteModelProvider(ctx context.Context, id string) error {
	cmd, err := s.pool.Exec(ctx,
		`DELETE FROM model_providers WHERE id = $1`, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrModelProviderReferenced
		}
		return fmt.Errorf("storage: удаление провайдера: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrModelProviderNotFound
	}
	return nil
}

// SetModelProviderDefaultModel обновляет default_model провайдера. Пустая
// строка допустима — адаптер откатится к своему встроенному дефолту.
// ErrModelProviderNotFound, если id не найден.
func (s *Storage) SetModelProviderDefaultModel(
	ctx context.Context, id, defaultModel string,
) (ModelProvider, error) {
	var p ModelProvider
	err := s.pool.QueryRow(ctx,
		`UPDATE model_providers SET default_model = $2, updated_at = now()
		   WHERE id = $1
		 RETURNING `+modelProviderColumns,
		id, defaultModel,
	).Scan(&p.ID, &p.Name, &p.TrustLevel, &p.Adapter, &p.Endpoint,
		&p.MaxTokens, &p.RateLimitPerMin, &p.IsEnabled,
		&p.APIKeyEncrypted, &p.DefaultModel, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModelProvider{}, ErrModelProviderNotFound
	}
	if err != nil {
		return ModelProvider{}, fmt.Errorf("storage: set default_model: %w", err)
	}
	return p, nil
}

// UpdateModelProviderAPIKey обновляет зашифрованный ключ провайдера.
// encrypted = nil/пусто → ключ становится NULL (использовать env-fallback).
// AAD при шифровании привязан к иммутабельному id (UUID, не name) —
// после Итерации 9.5 ребрендинг/rename провайдера НЕ ломает ключ.
func (s *Storage) UpdateModelProviderAPIKey(
	ctx context.Context, id string, encrypted []byte,
) error {
	var apiKey []byte
	if len(encrypted) > 0 {
		apiKey = encrypted
	}
	cmd, err := s.pool.Exec(ctx,
		`UPDATE model_providers SET api_key_encrypted = $1 WHERE id = $2`,
		apiKey, id)
	if err != nil {
		return fmt.Errorf("storage: обновление api_key: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrModelProviderNotFound
	}
	return nil
}

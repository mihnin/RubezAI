package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrModelProviderExists — провайдер модели с таким именем уже существует.
var ErrModelProviderExists = errors.New(
	"storage: провайдер модели с таким именем уже существует")

// ModelProvider — запись провайдера модели из таблицы model_providers.
// MaxTokens и RateLimitPerMin nullable: nil означает «без ограничения».
type ModelProvider struct {
	ID              string
	Name            string
	TrustLevel      string
	Adapter         string
	Endpoint        string
	MaxTokens       *int
	RateLimitPerMin *int
	IsEnabled       bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ListModelProviders возвращает провайдеров моделей, отсортированных по имени.
func (s *Storage) ListModelProviders(ctx context.Context) ([]ModelProvider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, trust_level, adapter, COALESCE(endpoint, ''),
		        max_tokens, rate_limit_per_min, is_enabled, created_at, updated_at
		 FROM model_providers ORDER BY name`)
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
			&p.CreatedAt, &p.UpdatedAt,
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

// CreateModelProvider создаёт провайдера модели. Пустой Endpoint сохраняется
// как NULL.
func (s *Storage) CreateModelProvider(
	ctx context.Context, input ModelProvider,
) (ModelProvider, error) {
	created := input
	err := s.pool.QueryRow(ctx,
		`INSERT INTO model_providers
		   (name, trust_level, adapter, endpoint, max_tokens, rate_limit_per_min)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
		 RETURNING id, is_enabled, created_at, updated_at`,
		input.Name, input.TrustLevel, input.Adapter, input.Endpoint,
		input.MaxTokens, input.RateLimitPerMin,
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

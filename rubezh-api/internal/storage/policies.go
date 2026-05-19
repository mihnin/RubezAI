package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrPolicyExists — политика с таким именем уже существует (нарушение UNIQUE).
var ErrPolicyExists = errors.New("storage: политика с таким именем уже существует")

// Policy — запись политики из таблицы policies.
type Policy struct {
	ID             string
	Name           string
	Description    string
	IsActive       bool
	CurrentVersion int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ListPolicies возвращает все политики, отсортированные по имени.
func (s *Storage) ListPolicies(ctx context.Context) ([]Policy, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, COALESCE(description, ''), is_active, current_version,
		        created_at, updated_at
		 FROM policies ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("storage: список политик: %w", err)
	}
	defer rows.Close()

	policies := make([]Policy, 0)
	for rows.Next() {
		var p Policy
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.IsActive, &p.CurrentVersion,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("storage: чтение строки политики: %w", err)
		}
		policies = append(policies, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: обход политик: %w", err)
	}
	return policies, nil
}

// CreatePolicy создаёт политику и её первую версию в одной транзакции.
func (s *Storage) CreatePolicy(
	ctx context.Context, name, description string,
) (Policy, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Policy{}, fmt.Errorf("storage: начало транзакции: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policy := Policy{Name: name, Description: description}
	err = tx.QueryRow(ctx,
		`INSERT INTO policies (name, description) VALUES ($1, $2)
		 RETURNING id, is_active, current_version, created_at, updated_at`,
		name, description,
	).Scan(&policy.ID, &policy.IsActive, &policy.CurrentVersion,
		&policy.CreatedAt, &policy.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Policy{}, ErrPolicyExists
		}
		return Policy{}, fmt.Errorf("storage: создание политики: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO policy_versions (policy_id, version, rules)
		 VALUES ($1, 1, '{}')`, policy.ID,
	); err != nil {
		return Policy{}, fmt.Errorf("storage: создание версии политики: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Policy{}, fmt.Errorf("storage: фиксация транзакции: %w", err)
	}
	return policy, nil
}

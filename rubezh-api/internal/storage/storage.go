// Package storage — слой доступа к PostgreSQL (пул соединений pgx).
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Storage — пул соединений с PostgreSQL.
type Storage struct {
	pool *pgxpool.Pool
}

// New открывает пул соединений по DSN. DSN валидируется немедленно.
func New(ctx context.Context, dsn string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: открытие пула: %w", err)
	}
	return &Storage{pool: pool}, nil
}

// Ping проверяет доступность БД.
func (s *Storage) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("storage: ping: %w", err)
	}
	return nil
}

// Pool возвращает пул соединений для слоёв выше.
func (s *Storage) Pool() *pgxpool.Pool {
	return s.pool
}

// Close закрывает пул соединений.
func (s *Storage) Close() {
	s.pool.Close()
}

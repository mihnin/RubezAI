package storage

import (
	"context"
	"testing"
)

func TestNewRejectsInvalidDSN(t *testing.T) {
	if _, err := New(context.Background(), "://не-валидный-dsn"); err == nil {
		t.Error("ожидалась ошибка при некорректном синтаксисе DSN")
	}
}

func TestNewAcceptsValidDSNWithoutConnecting(t *testing.T) {
	// pgxpool.New ленив: валидный по синтаксису DSN на недоступный хост
	// не вызывает ошибку — соединение устанавливается лениво (проверяет Ping).
	store, err := New(
		context.Background(),
		"postgres://u:p@nonexistent.invalid:5432/db?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("New с валидным синтаксисом DSN не должен падать: %v", err)
	}
	store.Close()
}

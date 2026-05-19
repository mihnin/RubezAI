package storage

import (
	"context"
	"testing"
)

func TestNewRejectsInvalidDSN(t *testing.T) {
	if _, err := New(context.Background(), "://не-валидный-dsn"); err == nil {
		t.Error("ожидалась ошибка при некорректном DSN")
	}
}

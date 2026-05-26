package api

import (
	"testing"
	"time"
)

func TestUserRateLimiterAllowsBurst(t *testing.T) {
	l := NewUserRateLimiter(30, 5)
	for i := 0; i < 5; i++ {
		ok, _ := l.Allow("user-1")
		if !ok {
			t.Errorf("первые 5 запросов (burst=5) должны пройти, отказ на %d", i)
		}
	}
}

func TestUserRateLimiterBlocksAfterBurst(t *testing.T) {
	l := NewUserRateLimiter(30, 3)
	// burst=3 → 3 проходят сразу
	for i := 0; i < 3; i++ {
		l.Allow("u")
	}
	// 4-й сразу же блокируется (rate=0.5 RPS, токен накопится через 2s)
	ok, audit := l.Allow("u")
	if ok {
		t.Error("4-й запрос (за burst) должен быть отвергнут")
	}
	if !audit {
		t.Error("первое превышение → shouldAudit=true")
	}
	// 5-й — тоже блок, но shouldAudit=false (anti-flood)
	ok2, audit2 := l.Allow("u")
	if ok2 {
		t.Error("5-й запрос блокирован")
	}
	if audit2 {
		t.Error("повторные превышения в одном окне НЕ audit'ятся (anti-flood)")
	}
}

func TestUserRateLimiterPerUserIsolation(t *testing.T) {
	l := NewUserRateLimiter(60, 2)
	// userA исчерпывает burst
	l.Allow("A")
	l.Allow("A")
	okA, _ := l.Allow("A")
	if okA {
		t.Error("userA должен быть заблокирован")
	}
	// userB не должен быть затронут
	okB, _ := l.Allow("B")
	if !okB {
		t.Error("userB не должен быть заблокирован лимитом userA")
	}
}

func TestUserRateLimiterEmptyUserIDPasses(t *testing.T) {
	l := NewUserRateLimiter(1, 1)
	// Без user_id (например, эндпойнт вне auth) — не лимитим.
	for i := 0; i < 100; i++ {
		ok, _ := l.Allow("")
		if !ok {
			t.Errorf("пустой userID не должен лимитироваться, отказ на %d", i)
		}
	}
}

func TestUserRateLimiterAuditOnePerWindow(t *testing.T) {
	l := NewUserRateLimiter(60, 1)
	l.Allow("u") // съедает burst
	// 9 подряд отвергнутых запросов → ровно 1 shouldAudit=true
	auditCount := 0
	for i := 0; i < 9; i++ {
		ok, audit := l.Allow("u")
		if ok {
			t.Fatalf("ожидался отказ на %d", i)
		}
		if audit {
			auditCount++
		}
	}
	if auditCount != 1 {
		t.Errorf("ожидался ровно 1 audit на 9 отказов, got %d", auditCount)
	}
}

func TestUserRateLimiterWindowResets(t *testing.T) {
	// Эмулируем reset окна через ручное обновление lastWindow.
	l := NewUserRateLimiter(60, 1)
	l.Allow("u")
	_, audit := l.Allow("u") // первое превышение → audit
	if !audit {
		t.Fatal("ожидался audit на 2-м запросе")
	}
	// «Перематываем» окно вручную для теста (вместо sleep'а 60s)
	l.mu.Lock()
	l.lastWindow = time.Now().Add(-2 * time.Minute)
	l.mu.Unlock()
	_, audit2 := l.Allow("u")
	if !audit2 {
		t.Error("после сброса окна audit должен сработать снова")
	}
}

func TestUserRateLimiterRetryAfterSeconds(t *testing.T) {
	l := NewUserRateLimiter(60, 1) // 1 RPS → retryAfter ≈ 1
	if got := l.RetryAfterSeconds(); got < 1 {
		t.Errorf("RetryAfterSeconds = %d, ожидалось ≥ 1", got)
	}
	l2 := NewUserRateLimiter(1, 1) // 1 RPM ≈ 0.0166 RPS → 60s
	if got := l2.RetryAfterSeconds(); got != 60 {
		t.Errorf("1 RPM: RetryAfterSeconds = %d, ожидалось 60", got)
	}
}

package api

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// UserRateLimiter — token-bucket per user_id (Итерация 11 §Р6).
//
// Назначение: anti-bulk-exfiltration для RAG-эндпойнтов (/api/search и
// /api/chat при rag.enabled=true). 30 запросов/минуту на пользователя
// (burst=5). Превышение → handler возвращает 429 с заголовком
// Retry-After и пишет audit_event `rate_limit_exceeded` ОДИН РАЗ
// в окне (anti-flood журнала).
//
// KNOWN LIMITATION (план §Р6): in-memory, не переживает restart процесса.
// Для одно-инстансного on-prem MVP приемлемо; распределённый rate-limit
// (Redis token bucket или Postgres advisory locks) — пост-MVP.
//
// KNOWN LIMITATION 2 (план §Р6): sync.Map без GC. Для long-running
// процесса с миллионами уникальных user_id течёт ~64 байт на bucket.
// В MVP (десятки активных пользователей) неактуально.
type UserRateLimiter struct {
	rps      rate.Limit // запросов в секунду
	burst    int        // размер bucket
	mu       sync.Mutex // защищает limiters + reported
	limiters map[string]*rate.Limiter
	// reported — пользователи, для которых уже записан `rate_limit_exceeded`
	// в текущем окне. Сбрасывается каждый windowDur через resetReportedLoop.
	reported   map[string]bool
	windowDur  time.Duration
	lastWindow time.Time
}

// NewUserRateLimiter создаёт limiter с заданным RPM на пользователя и burst.
// windowSeconds — длина окна для anti-flood audit (по умолчанию 60s = 1 минута,
// синхронно с RPM-окном).
func NewUserRateLimiter(rpm int, burst int) *UserRateLimiter {
	return &UserRateLimiter{
		rps:        rate.Limit(float64(rpm) / 60.0),
		burst:      burst,
		limiters:   make(map[string]*rate.Limiter),
		reported:   make(map[string]bool),
		windowDur:  time.Minute,
		lastWindow: time.Now(),
	}
}

// Allow проверяет, разрешён ли запрос для userID. Возвращает (allowed,
// shouldAudit). shouldAudit=true только при ПЕРВОМ отвергнутом запросе
// в текущем 1-минутном окне для этого пользователя (anti-flood).
func (l *UserRateLimiter) Allow(userID string) (allowed, shouldAudit bool) {
	if userID == "" {
		return true, false // без авторизации rate-limit не применяем
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	// Сброс reported при переходе в новое окно.
	if time.Since(l.lastWindow) >= l.windowDur {
		l.reported = make(map[string]bool)
		l.lastWindow = time.Now()
	}

	lim, ok := l.limiters[userID]
	if !ok {
		lim = rate.NewLimiter(l.rps, l.burst)
		l.limiters[userID] = lim
	}
	if lim.Allow() {
		return true, false
	}
	if l.reported[userID] {
		return false, false
	}
	l.reported[userID] = true
	return false, true
}

// RetryAfterSeconds — рекомендованная задержка перед повтором (в секундах).
// Для token-bucket это время до накопления 1 токена.
func (l *UserRateLimiter) RetryAfterSeconds() int {
	if l.rps == 0 {
		return 60
	}
	sec := int(1.0 / float64(l.rps))
	if sec < 1 {
		return 1
	}
	return sec
}

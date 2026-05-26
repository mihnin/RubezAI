package chat

import (
	"sync"
	"time"
)

// throttleReporter — rate-limit вспомогательных audit-событий per user
// (план Итерации 11 §Р4). Допускает не более `limit` событий за окно
// `window` на пользователя. По исчерпании Allow() возвращает (false, true)
// один раз за окно — это сигнал «пиши _throttled-event»; затем (false, false)
// до следующего окна. Внутри окна Allow для разрешённых событий
// возвращает (true, false).
//
// Используется для policy_revised_after_rag (10/час) — без него хватит
// одного «талантливого» retrieval-чанка, чтобы шумно поднимать аудит-журнал.
//
// Реализация in-memory + sync.Mutex; для on-prem MVP с одним инстансом
// API-сервиса этого достаточно. При горизонтальном масштабировании ⇒
// перенести в Postgres advisory locks.
type throttleReporter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	state    map[string]*throttleState
}

type throttleState struct {
	count     int
	throttled bool
	resetAt   time.Time
}

func newThrottleReporter(limit int, window time.Duration) *throttleReporter {
	return &throttleReporter{
		limit: limit, window: window,
		state: make(map[string]*throttleState),
	}
}

// Allow возвращает (allowed, shouldEmitThrottled):
//   - allowed=true  → допустимо записать основной audit-event;
//   - allowed=false, shouldEmitThrottled=true → лимит исчерпан впервые
//     в этом окне; вызывающему стоит записать _throttled-event;
//   - allowed=false, shouldEmitThrottled=false → лимит исчерпан и уже
//     был сообщён в этом окне; ничего писать не нужно.
func (r *throttleReporter) Allow(userID string) (allowed, shouldEmitThrottled bool) {
	if r == nil {
		return true, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	st, ok := r.state[userID]
	if !ok || now.After(st.resetAt) {
		st = &throttleState{resetAt: now.Add(r.window)}
		r.state[userID] = st
	}
	if st.count < r.limit {
		st.count++
		return true, false
	}
	if st.throttled {
		return false, false
	}
	st.throttled = true
	return false, true
}

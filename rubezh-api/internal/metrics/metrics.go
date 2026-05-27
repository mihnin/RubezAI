// Package metrics — Prometheus-инструментация rubezh-api (W4.1).
//
// Метрики выставляются на GET /metrics (без auth, без TLS). По плану
// деплоя on-prem rubezh-api сидит за внутренним nginx/Traefik;
// scrape-trafic считается внутренним и не покидает периметр. Если
// нужно ограничить — добавьте rule в reverse proxy на /metrics path.
//
// Префикс всех custom-метрик: `rubezh_api_*`. Стандартные `go_*` и
// `process_*` экспортируются автоматически через collectors.Default.
//
// Cardinality: лейблы выбраны узкими, чтобы не разрывать индекс
// Prometheus. provider — на out-of-band ставится как `provider.Name`
// (есть конечное множество, обычно ≤20). decision — фиксированный
// enum policy.Decision (5 значений). role — RoleFromContext (6 ролей).
// event_type — фиксированный enum chat_request/chat_response/...
// (~10 значений). stage — для throttle (~5 значений).
package metrics

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics — собранный набор регистрируемых метрик. Создаётся один раз
// в main.go и прокидывается в Deps; конкретные хендлеры дёргают
// .Observe()/.Inc() в своих узловых точках.
type Metrics struct {
	// ChatRequestsTotal — счётчик POST /api/chat (терминальный — после
	// done|error). Лейблы:
	//   - decision: allow_raw | allow_masked | allow_summary_only | deny | escalate
	//   - provider: provider.Name из БД (узкое множество)
	//   - outcome:  ok | error (ошибка SSE/LLM)
	ChatRequestsTotal *prometheus.CounterVec
	// ChatRequestDurationSeconds — длительность Prepare+Stream POST /api/chat
	// (от первого byte до done|error). Histogram c default-бакетами Prometheus.
	ChatRequestDurationSeconds *prometheus.HistogramVec
	// AuditEventsTotal — счётчик записанных audit-events (RecordAuditEvent
	// + auditEvent в RecordChatRequest/Termination). Лейблы:
	//   - event_type: chat_request | chat_response | chat_blocked | chat_error |
	//                 incident_created_auto | response_revealed | …
	AuditEventsTotal *prometheus.CounterVec
	// SanitizeFailuresTotal — счётчик ошибок sanitize. Лейблы:
	//   - stage: chat | document | system_prompt | review_system_prompt |
	//            preview_token_miss | preview_token_miss_throttled
	//   - reason: timeout | network | unknown
	SanitizeFailuresTotal *prometheus.CounterVec
	// ThrottleEventsTotal — счётчик срабатываний throttle-reporter'ов.
	// Лейблы:
	//   - kind: rag_policy_revised | preview_token_miss
	//   - outcome: allowed | throttled
	ThrottleEventsTotal *prometheus.CounterVec
	// LLMRouterProviders — текущее число зарегистрированных LLM-провайдеров
	// в Router'е (обновляется при Replace() в hot-reload). Gauge.
	LLMRouterProviders prometheus.Gauge

	// registry — изолированный реестр; даёт детерминизм в тестах и
	// предотвращает конфликт с глобальным prometheus.DefaultRegisterer
	// (например, при двух экземплярах в test-процессе).
	registry *prometheus.Registry
	// scrapeBearer — опциональный Bearer-token для защиты /metrics.
	// Пусто → endpoint открыт (on-prem за периметром); непусто → требует
	// заголовка `Authorization: Bearer <value>`. Constant-time compare.
	scrapeBearer string
}

// New создаёт и регистрирует все метрики в свежем реестре. Один экземпляр
// должен существовать на процесс; main.go создаёт его и прокидывает дальше.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	// Default Go runtime + process метрики — стандартный набор.
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
	m := &Metrics{
		ChatRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rubezh_api_chat_requests_total",
			Help: "POST /api/chat — терминальные ответы по decision/provider/outcome.",
		}, []string{"decision", "provider", "outcome"}),
		ChatRequestDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rubezh_api_chat_request_duration_seconds",
			Help:    "Длительность POST /api/chat (Prepare+Stream до done|error).",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider"}),
		AuditEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rubezh_api_audit_events_total",
			Help: "Счётчик записанных audit-events по event_type.",
		}, []string{"event_type"}),
		SanitizeFailuresTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rubezh_api_sanitize_failures_total",
			Help: "Ошибки sanitize по stage/reason (для алертинга при сбоях фильтра).",
		}, []string{"stage", "reason"}),
		ThrottleEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rubezh_api_throttle_events_total",
			Help: "Срабатывания throttle-reporter (preview_token_miss, rag_policy_revised).",
		}, []string{"kind", "outcome"}),
		LLMRouterProviders: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rubezh_api_llm_router_providers",
			Help: "Текущее число зарегистрированных LLM-провайдеров.",
		}),
		registry: reg,
	}
	reg.MustRegister(
		m.ChatRequestsTotal,
		m.ChatRequestDurationSeconds,
		m.AuditEventsTotal,
		m.SanitizeFailuresTotal,
		m.ThrottleEventsTotal,
		m.LLMRouterProviders,
	)
	return m
}

// Handler возвращает HTTP-обработчик для GET /metrics в Prometheus
// text-exposition-формате.
//
// W4.5 MJ-1: если в env задан `METRICS_AUTH_BEARER`, endpoint требует
// `Authorization: Bearer <value>` (constant-time compare). Это закрывает
// внешнюю утечку internal-telemetry, когда reverse proxy не настроен
// или development-режим выставил `/metrics` за публичный nginx.
// Пустой env → endpoint остаётся открытым (on-prem за периметром).
func (m *Metrics) Handler() http.Handler {
	base := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
	expected := m.scrapeBearer
	if expected == "" {
		return base
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if !verifyScrapeBearer(got, expected) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "metrics scrape forbidden", http.StatusUnauthorized)
			return
		}
		base.ServeHTTP(w, r)
	})
}

// Registry экспортирует регистратор для отладки/тестов (например,
// dump через registry.Gather()). В production не используется.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// WithScrapeBearer защищает /metrics опциональным Bearer-token'ом
// (W4.5 MJ-1). Пустая строка — endpoint остаётся открытым (default).
// Возвращает receiver для chain'а в main.go.
func (m *Metrics) WithScrapeBearer(token string) *Metrics {
	m.scrapeBearer = token
	return m
}

// verifyScrapeBearer — constant-time проверка `Authorization: Bearer X`.
// Игнорирует case у схемы и whitespace; subtle.ConstantTimeCompare против
// timing-атак на токен.
func verifyScrapeBearer(authHeader, expected string) bool {
	if expected == "" {
		return true
	}
	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return false
	}
	got := strings.TrimSpace(parts[1])
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

// Реализация chat.MetricsRecorder (узкий интерфейс — см.
// internal/chat/metrics.go). Прокладка не объявлена явно через
// `var _ chat.MetricsRecorder = (*Metrics)(nil)`, чтобы избежать
// цикла chat ↔ metrics; типовая совместимость проверяется в main.go
// при подстановке `orchestrator.WithMetrics(metricsCollector)`.

// IncAuditEvent — rubezh_api_audit_events_total{event_type}.
func (m *Metrics) IncAuditEvent(eventType string) {
	m.AuditEventsTotal.WithLabelValues(eventType).Inc()
}

// IncSanitizeFailure — rubezh_api_sanitize_failures_total{stage,reason}.
func (m *Metrics) IncSanitizeFailure(stage, reason string) {
	m.SanitizeFailuresTotal.WithLabelValues(stage, reason).Inc()
}

// IncThrottleEvent — rubezh_api_throttle_events_total{kind,outcome}.
func (m *Metrics) IncThrottleEvent(kind, outcome string) {
	m.ThrottleEventsTotal.WithLabelValues(kind, outcome).Inc()
}

// IncChatRequest — rubezh_api_chat_requests_total{decision,provider,outcome}.
func (m *Metrics) IncChatRequest(decision, provider, outcome string) {
	m.ChatRequestsTotal.WithLabelValues(decision, provider, outcome).Inc()
}

// ObserveChatDuration — rubezh_api_chat_request_duration_seconds{provider}.
func (m *Metrics) ObserveChatDuration(provider string, seconds float64) {
	m.ChatRequestDurationSeconds.WithLabelValues(provider).Observe(seconds)
}

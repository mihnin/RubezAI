package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMetricsHandlerExposesCustomMetrics — после Inc'ов на свежесозданном
// Metrics экспозиция /metrics должна включать наши rubezh_api_* серии.
func TestMetricsHandlerExposesCustomMetrics(t *testing.T) {
	m := New()
	m.IncAuditEvent("chat_request")
	m.IncSanitizeFailure("chat", "timeout")
	m.IncThrottleEvent("preview_token_miss", "throttled")
	m.IncChatRequest("allow_masked", "claude-code-cli", "ok")
	m.ObserveChatDuration("claude-code-cli", 0.42)
	m.LLMRouterProviders.Set(7)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("code = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`rubezh_api_audit_events_total{event_type="chat_request"} 1`,
		`rubezh_api_sanitize_failures_total{reason="timeout",stage="chat"} 1`,
		`rubezh_api_throttle_events_total{kind="preview_token_miss",outcome="throttled"} 1`,
		`rubezh_api_chat_requests_total{decision="allow_masked",outcome="ok",provider="claude-code-cli"} 1`,
		`rubezh_api_chat_request_duration_seconds_count{provider="claude-code-cli"} 1`,
		`rubezh_api_llm_router_providers 7`,
		// Стандартные коллекторы — без них экспозиция была бы неполной.
		`go_gc_duration_seconds`,
		`process_cpu_seconds_total`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output не содержит %q\n--- body ---\n%s",
				want, body)
		}
	}
}

// TestMetricsDuplicateRegistrationSafe — N экземпляров Metrics в одном
// процессе не должны паниковать (изолированный Registry — инвариант).
func TestMetricsDuplicateRegistrationSafe(t *testing.T) {
	_ = New()
	_ = New() // если бы регистратор был глобальный — паника из MustRegister.
}

// W4.5 MJ-1: при заданном scrapeBearer endpoint требует Authorization.
func TestMetricsScrapeBearerEnforced(t *testing.T) {
	m := New().WithScrapeBearer("s3cret")

	// Без заголовка → 401.
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("no auth → code=%d, ожидалось 401", rec.Code)
	}

	// Неверный токен → 401.
	req = httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("wrong token → code=%d, ожидалось 401", rec.Code)
	}

	// Верный токен → 200 + content.
	req = httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("valid token → code=%d, ожидалось 200", rec.Code)
	}
}

func TestMetricsScrapeBearerEmptyTokenOpen(t *testing.T) {
	// Пустой токен (default) → endpoint открыт.
	m := New() // без WithScrapeBearer
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("default → code=%d, ожидалось 200", rec.Code)
	}
}

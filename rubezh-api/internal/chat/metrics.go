package chat

// MetricsRecorder — узкий контракт инструментации для orchestrator'а.
// Реализуется в internal/metrics; здесь — интерфейс, чтобы избежать
// импорта Prometheus-client'а из chat-пакета и не плодить циклы.
//
// Все методы — best-effort, не должны бросать или блокировать. nil-
// receiver безопасен (см. WithMetrics).
type MetricsRecorder interface {
	IncAuditEvent(eventType string)
	IncSanitizeFailure(stage, reason string)
	IncThrottleEvent(kind, outcome string) // outcome: allowed | throttled
	IncChatRequest(decision, provider, outcome string)
	ObserveChatDuration(provider string, seconds float64)
}

// noopMetrics — используется, когда orchestrator создан без metrics
// (тесты, legacy-вызовы). Все методы — no-op.
type noopMetrics struct{}

func (noopMetrics) IncAuditEvent(string)                  {}
func (noopMetrics) IncSanitizeFailure(string, string)     {}
func (noopMetrics) IncThrottleEvent(string, string)       {}
func (noopMetrics) IncChatRequest(string, string, string) {}
func (noopMetrics) ObserveChatDuration(string, float64)   {}

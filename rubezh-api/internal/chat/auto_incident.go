package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// triggerKind — причина авто-создания инцидента.
type triggerKind string

const (
	triggerDeny     triggerKind = "deny"
	triggerEscalate triggerKind = "escalate"
	triggerLeak     triggerKind = "response_leak_detected"
)

// autoIncidentTrigger возвращает trigger для авто-инцидента либо ""
// если инцидент не нужен. detail — terminal-аудит-событие (Tx2).
// План iteration-9.md §Р4.
func autoIncidentTrigger(decision string, leakDetected bool) triggerKind {
	switch {
	case decision == "deny":
		return triggerDeny
	case decision == "escalate":
		return triggerEscalate
	case leakDetected:
		return triggerLeak
	}
	return ""
}

// severityFor отображает (risk_level, trigger) → severity инцидента.
// Закрывает MINOR-M5 ревью v2: leak повышает на ДВЕ ступени для
// low/medium (компрометация маскирования серьёзнее обычного deny).
func severityFor(riskLevel string, trigger triggerKind) string {
	if trigger == triggerLeak {
		switch riskLevel {
		case "low":
			return "high"
		case "medium":
			return "critical"
		case "high", "critical":
			return "critical"
		default:
			return "high"
		}
	}
	// deny/escalate — severity = risk_level
	switch riskLevel {
	case "low", "medium", "high", "critical":
		return riskLevel
	default:
		return "medium"
	}
}

// autoIncidentTitle — человекочитаемое название авто-инцидента.
func autoIncidentTitle(trigger triggerKind, riskLevel string) string {
	switch trigger {
	case triggerDeny:
		return fmt.Sprintf("Авто: блокировка запроса (риск %s)", riskLevel)
	case triggerEscalate:
		return fmt.Sprintf("Авто: эскалация запроса (риск %s)", riskLevel)
	case triggerLeak:
		return fmt.Sprintf("Авто: модель воспроизвела замаскированное значение (риск %s)", riskLevel)
	}
	return "Авто-инцидент"
}

// autoIncidentSummary — короткое описание для UI и аудита.
func autoIncidentSummary(
	trigger triggerKind, riskLevel string, classes []string,
) string {
	switch trigger {
	case triggerDeny, triggerEscalate:
		return fmt.Sprintf(
			"Policy engine принял решение %s. Уровень риска: %s. Классы: %v.",
			trigger, riskLevel, classes)
	case triggerLeak:
		return fmt.Sprintf(
			"Модель воспроизвела одно из замаскированных значений в ответе. "+
				"Уровень риска: %s. Классы: %v. См. detail.leaked_pseudonyms в audit.",
			riskLevel, classes)
	}
	return ""
}

// createAutoIncidentIfNeeded — вызывает Store.CreateAutoIncident если
// trigger ≠ "". Пишет audit-event incident_create_failed при ошибке
// (кроме ErrIncidentAutoDuplicate, который означает «уже есть»).
//
// Закрывает MAJOR-1 (race-safe partial unique), M4 (atomic Tx3) и
// добавляет audit-след для каждого auto-incident'а.
func (o *Orchestrator) createAutoIncidentIfNeeded(
	ctx context.Context, req Request, riskLevel string, classes []string,
	terminationAuditID string, leakDetected bool, decision string,
) {
	trigger := autoIncidentTrigger(decision, leakDetected)
	if trigger == "" {
		return
	}
	sev := severityFor(riskLevel, trigger)
	title := autoIncidentTitle(trigger, riskLevel)
	summary := autoIncidentSummary(trigger, riskLevel, classes)
	userID := req.UserID

	auditCtx, cancel := withDetachedTimeout(ctx)
	defer cancel()

	// AuditEvent для incident_created_auto идёт ВНУТРИ Tx3
	// (Atomic Tx3, M4 ревью v2): incident + audit пишутся атомарно.
	createAuditEvent := storage.AuditEvent{
		UserID:    userID,
		EventType: "incident_created_auto",
		Detail: map[string]any{
			"request_id":     req.RequestID,
			"trigger":        string(trigger),
			"audit_event_id": terminationAuditID,
			"severity":       sev,
		},
	}

	auditEventIDPtr := &terminationAuditID
	_, _, err := o.store.CreateAutoIncident(auditCtx,
		storage.IncidentInput{
			AuditEventID: auditEventIDPtr,
			UserID:       &userID,
			ReporterID:   nil, // auto
			Severity:     sev,
			Status:       "open",
			Title:        title,
			Summary:      &summary,
		},
		createAuditEvent,
	)
	if err == nil {
		return
	}
	// Дубликат — нормальная race-ситуация, ничего не пишем.
	if errors.Is(err, storage.ErrIncidentAutoDuplicate) {
		return
	}
	// Иная ошибка — фиксируем отдельным event-типом (не chat_error,
	// потому что чат уже прошёл успешно). План §Р4 + M4 ревью v2.
	o.recordAuditEvent(ctx, storage.AuditEvent{
		UserID:    userID,
		EventType: "incident_create_failed",
		Detail: map[string]any{
			"request_id":     req.RequestID,
			"trigger":        string(trigger),
			"audit_event_id": terminationAuditID,
			"error":          err.Error(),
		},
	})
}

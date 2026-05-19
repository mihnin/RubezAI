package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AuditEvent — запись журнала аудита (таблица audit_events, append-only).
// EventType ∈ {chat_request, chat_response, chat_blocked, chat_error}.
type AuditEvent struct {
	UserID          string
	EventType       string
	ModelProviderID *string
	RiskLevel       *string
	RiskClasses     []string
	PolicyDecision  *string
	PolicyVersionID *string
	MatchedRule     *string
	MaskedPayload   *string
	Detail          map[string]any
}

// rowQuerier — общий интерфейс пула и транзакции для INSERT ... RETURNING.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// InsertAuditEvent записывает событие в журнал аудита (вне транзакции).
func (s *Storage) InsertAuditEvent(
	ctx context.Context, ev AuditEvent,
) (string, error) {
	return insertAuditEvent(ctx, s.pool, ev)
}

// insertAuditEvent вставляет audit-событие через переданный querier
// (пул либо транзакция) и возвращает id.
func insertAuditEvent(
	ctx context.Context, q rowQuerier, ev AuditEvent,
) (string, error) {
	classes := ev.RiskClasses
	if classes == nil {
		classes = []string{}
	}
	detail := ev.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("storage: сериализация detail аудита: %w", err)
	}

	var id string
	err = q.QueryRow(ctx,
		`INSERT INTO audit_events
		   (user_id, event_type, model_provider_id, risk_level, risk_classes,
		    policy_decision, policy_version_id, matched_rule, masked_payload,
		    detail)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 RETURNING id`,
		ev.UserID, ev.EventType, ev.ModelProviderID, ev.RiskLevel, classes,
		ev.PolicyDecision, ev.PolicyVersionID, ev.MatchedRule, ev.MaskedPayload,
		json.RawMessage(detailJSON),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("storage: запись audit-события: %w", err)
	}
	return id, nil
}

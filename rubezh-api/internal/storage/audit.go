package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// AuditEvent — запись журнала аудита (таблица audit_events, append-only).
// EventType ∈ {chat_request, chat_response, chat_blocked, chat_error,
// policy_tested, model_*, incident_*, audit_exported, auth_*} —
// полный enum в docs/contracts/audit.schema.json.
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

// AuditEventRow — прочитанная запись (с id и created_at). Detail
// возвращается как json.RawMessage — потребитель десериализует по
// надобности (audit_events.detail jsonb может содержать произвольные ключи).
type AuditEventRow struct {
	ID              string
	CreatedAt       time.Time
	UserID          string
	EventType       string
	ModelProviderID *string
	RiskLevel       *string
	RiskClasses     []string
	PolicyDecision  *string
	PolicyVersionID *string
	MatchedRule     *string
	MaskedPayload   *string
	Detail          json.RawMessage
}

// HasLeak проверяет detail.response_leak_detected = true. Удобно для UI
// и для тестов; SQL-фильтр работает через GIN-индекс.
func (r AuditEventRow) HasLeak() bool {
	if len(r.Detail) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(r.Detail, &m); err != nil {
		return false
	}
	v, _ := m["response_leak_detected"].(bool)
	return v
}

// ErrAuditEventNotFound — нет события с указанным id (или вне scope).
var ErrAuditEventNotFound = errors.New("storage: audit-event не найден")

// AuditFilter — фильтры ListAuditEvents. Все поля опциональны.
// CursorCreatedAt + CursorID реализуют keyset row-comparison.
type AuditFilter struct {
	From, To        *time.Time
	UserID          *string
	EventTypes      []string
	PolicyDecisions []string
	RiskLevels      []string
	ModelProviderID *string
	HasLeak         *bool   // nil = все; true = только с утечкой; false = без утечки
	Q               *string // ILIKE по masked_payload
	CursorCreatedAt *time.Time
	CursorID        *string
	Limit           int // 1..200, дефолт 50
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

// auditEventColumns — список колонок для SELECT'а (поддерживается в одном месте).
const auditEventColumns = `id, created_at, user_id, event_type,
	model_provider_id, risk_level, risk_classes, policy_decision,
	policy_version_id, matched_rule, masked_payload, detail`

// GetAuditEvent читает audit-событие по id.
func (s *Storage) GetAuditEvent(
	ctx context.Context, id string,
) (AuditEventRow, error) {
	var r AuditEventRow
	err := s.pool.QueryRow(ctx,
		`SELECT `+auditEventColumns+` FROM audit_events WHERE id = $1`, id,
	).Scan(&r.ID, &r.CreatedAt, &r.UserID, &r.EventType,
		&r.ModelProviderID, &r.RiskLevel, &r.RiskClasses,
		&r.PolicyDecision, &r.PolicyVersionID, &r.MatchedRule,
		&r.MaskedPayload, &r.Detail)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuditEventRow{}, ErrAuditEventNotFound
	}
	if err != nil {
		return AuditEventRow{}, fmt.Errorf("storage: get audit-event: %w", err)
	}
	return r, nil
}

// ListAuditEvents возвращает audit-события по фильтру с keyset cursor.
// SQL — row-comparison (created_at, id) < ($1, $2) — стабильно к INSERT'ам
// при сортировке (created_at DESC, id DESC).
func (s *Storage) ListAuditEvents(
	ctx context.Context, f AuditFilter,
) ([]AuditEventRow, error) {
	q, args := buildListAuditQuery(f)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list audit-events: %w", err)
	}
	defer rows.Close()
	var out []AuditEventRow
	for rows.Next() {
		var r AuditEventRow
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.UserID, &r.EventType,
			&r.ModelProviderID, &r.RiskLevel, &r.RiskClasses,
			&r.PolicyDecision, &r.PolicyVersionID, &r.MatchedRule,
			&r.MaskedPayload, &r.Detail); err != nil {
			return nil, fmt.Errorf("storage: scan audit-event: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// buildListAuditQuery формирует SQL и аргументы для ListAuditEvents.
// Вынесено отдельно для тестов структуры SQL и переиспользования в export.
func buildListAuditQuery(f AuditFilter) (string, []any) {
	args := []any{}
	conds := []string{}
	add := func(c string, val ...any) {
		args = append(args, val...)
		conds = append(conds, c)
	}
	if f.CursorCreatedAt != nil && f.CursorID != nil {
		add(fmt.Sprintf(
			"(created_at, id) < ($%d::timestamptz, $%d::uuid)",
			len(args)+1, len(args)+2),
			*f.CursorCreatedAt, *f.CursorID)
	}
	if f.From != nil {
		add(fmt.Sprintf("created_at >= $%d", len(args)+1), *f.From)
	}
	if f.To != nil {
		add(fmt.Sprintf("created_at <= $%d", len(args)+1), *f.To)
	}
	if f.UserID != nil {
		add(fmt.Sprintf("user_id = $%d::uuid", len(args)+1), *f.UserID)
	}
	if len(f.EventTypes) > 0 {
		add(fmt.Sprintf("event_type = ANY($%d::text[])", len(args)+1),
			f.EventTypes)
	}
	if len(f.PolicyDecisions) > 0 {
		add(fmt.Sprintf("policy_decision = ANY($%d::text[])", len(args)+1),
			f.PolicyDecisions)
	}
	if len(f.RiskLevels) > 0 {
		add(fmt.Sprintf("risk_level = ANY($%d::text[])", len(args)+1),
			f.RiskLevels)
	}
	if f.ModelProviderID != nil {
		add(fmt.Sprintf("model_provider_id = $%d::uuid", len(args)+1),
			*f.ModelProviderID)
	}
	if f.HasLeak != nil {
		if *f.HasLeak {
			conds = append(conds,
				"(detail->>'response_leak_detected') = 'true'")
		} else {
			conds = append(conds,
				"COALESCE((detail->>'response_leak_detected'), 'false') = 'false'")
		}
	}
	if f.Q != nil && *f.Q != "" {
		add(fmt.Sprintf("masked_payload ILIKE $%d", len(args)+1),
			"%"+*f.Q+"%")
	}

	q := `SELECT ` + auditEventColumns + ` FROM audit_events`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d",
		len(args))
	return q, args
}

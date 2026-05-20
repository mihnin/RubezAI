package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// (json и encoding/json используются в DTO Filters / detail).

// auditAccessRoles — роли, имеющие полный доступ к audit-events.
// Контракт audit.schema.json + план iteration-9.md §Р3.
var auditAccessRoles = map[auth.Role]bool{
	auth.RoleSecurityOfficer:   true,
	auth.RoleComplianceOfficer: true,
	auth.RoleAuditor:           true,
	auth.RoleAdmin:             true,
}

// auditCursor — структура keyset-cursor для list-эндпойнта. Сериализуется
// в base64-JSON и непрозрачен для клиента. Соответствует SQL-форме
// (created_at, id) < ($1, $2) — row-comparison.
type auditCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"i"`
}

func encodeAuditCursor(c auditCursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeAuditCursor(raw string) (*auditCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var c auditCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// auditEventSummaryDTO — облегчённое представление для list.
type auditEventSummaryDTO struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	UserID          string    `json:"user_id"`
	EventType       string    `json:"event_type"`
	ModelProviderID *string   `json:"model_provider_id"`
	RiskLevel       *string   `json:"risk_level"`
	RiskClasses     []string  `json:"risk_classes"`
	PolicyDecision  *string   `json:"policy_decision"`
	RequestID       *string   `json:"request_id"`
	HasLeak         bool      `json:"has_leak"`
}

type auditEventDetailDTO struct {
	auditEventSummaryDTO
	PolicyVersionID *string         `json:"policy_version_id"`
	MatchedRule     *string         `json:"matched_rule"`
	MaskedPayload   *string         `json:"masked_payload"`
	Detail          json.RawMessage `json:"detail"`
}

type auditEventListDTO struct {
	Events     []auditEventSummaryDTO `json:"events"`
	NextCursor *string                `json:"next_cursor"`
}

// requireAuditAccess проверяет роль; для developer возвращает true с
// scope=true (нужен хардфильтр); для всех других — false=нет доступа.
func requireAuditAccess(role auth.Role) (allowed, devScope bool) {
	if auditAccessRoles[role] {
		return true, false
	}
	if role == auth.RoleDeveloper {
		return true, true
	}
	return false, false
}

// extractRequestID достаёт request_id из jsonb detail (если есть).
func extractRequestID(detail json.RawMessage) *string {
	if len(detail) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(detail, &m); err != nil {
		return nil
	}
	if v, ok := m["request_id"].(string); ok && v != "" {
		return &v
	}
	return nil
}

// rowToSummary — конверсия storage row → API DTO (облегчённое).
func rowToSummary(r storage.AuditEventRow) auditEventSummaryDTO {
	classes := r.RiskClasses
	if classes == nil {
		classes = []string{}
	}
	return auditEventSummaryDTO{
		ID: r.ID, CreatedAt: r.CreatedAt, UserID: r.UserID,
		EventType: r.EventType, ModelProviderID: r.ModelProviderID,
		RiskLevel: r.RiskLevel, RiskClasses: classes,
		PolicyDecision: r.PolicyDecision,
		RequestID:      extractRequestID(r.Detail),
		HasLeak:        r.HasLeak(),
	}
}

func rowToDetail(r storage.AuditEventRow) auditEventDetailDTO {
	detail := r.Detail
	if len(detail) == 0 {
		detail = json.RawMessage("{}")
	}
	return auditEventDetailDTO{
		auditEventSummaryDTO: rowToSummary(r),
		PolicyVersionID:      r.PolicyVersionID,
		MatchedRule:          r.MatchedRule,
		MaskedPayload:        r.MaskedPayload,
		Detail:               detail,
	}
}

// listAuditEventsHandler — GET /api/audit-events.
func listAuditEventsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		allowed, devScope := requireAuditAccess(role)
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		filter, err := parseAuditFilter(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if devScope {
			// Хардфильтр: developer видит только свои policy_tested.
			userID, e := currentUserID(r, store)
			if e != nil {
				http.Error(w, "user not resolved",
					http.StatusInternalServerError)
				return
			}
			filter.UserID = &userID
			filter.EventTypes = []string{"policy_tested"}
		}
		rows, err := store.ListAuditEvents(r.Context(), filter)
		if err != nil {
			http.Error(w, "ошибка чтения аудита",
				http.StatusInternalServerError)
			return
		}
		out := assembleAuditList(rows, filter.Limit)
		writeJSON(w, http.StatusOK, out)
	}
}

// parseAuditFilter читает query-параметры в storage.AuditFilter.
func parseAuditFilter(r *http.Request) (storage.AuditFilter, error) {
	q := r.URL.Query()
	f := storage.AuditFilter{
		EventTypes:      q["event_type"],
		PolicyDecisions: q["policy_decision"],
		RiskLevels:      q["risk_level"],
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errors.New("from: невалидный RFC3339")
		}
		f.From = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errors.New("to: невалидный RFC3339")
		}
		f.To = &t
	}
	if v := q.Get("user_id"); v != "" {
		if !isUUID(v) {
			return f, errors.New("user_id: невалидный UUID")
		}
		f.UserID = &v
	}
	if v := q.Get("model_provider_id"); v != "" {
		if !isUUID(v) {
			return f, errors.New("model_provider_id: невалидный UUID")
		}
		f.ModelProviderID = &v
	}
	if v := q.Get("has_leak"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, errors.New("has_leak: ожидался true/false")
		}
		f.HasLeak = &b
	}
	if v := q.Get("q"); v != "" {
		f.Q = &v
	}
	if v := q.Get("cursor"); v != "" {
		c, err := decodeAuditCursor(v)
		if err != nil {
			return f, errors.New("cursor: невалидный")
		}
		if c != nil {
			f.CursorCreatedAt = &c.CreatedAt
			f.CursorID = &c.ID
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return f, errors.New("limit: 1..200")
		}
		f.Limit = n
	} else {
		f.Limit = 50
	}
	return f, nil
}

// assembleAuditList запрашивает limit+1, чтобы определить next_cursor.
// На вход — уже прочитанные rows (storage возвращает до Limit). Для
// keyset-pagination правильное решение — запросить limit+1 в storage,
// здесь же мы делаем сборку без оверхеда: next_cursor = из последней
// строки, если len(rows) == limit (последняя страница не отличима без
// перезапроса; это компромисс MVP).
func assembleAuditList(
	rows []storage.AuditEventRow, limit int,
) auditEventListDTO {
	events := make([]auditEventSummaryDTO, 0, len(rows))
	for _, r := range rows {
		events = append(events, rowToSummary(r))
	}
	var next *string
	if len(rows) >= limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		cur := encodeAuditCursor(auditCursor{
			CreatedAt: last.CreatedAt, ID: last.ID,
		})
		next = &cur
	}
	return auditEventListDTO{Events: events, NextCursor: next}
}

// getAuditEventHandler — GET /api/audit-events/:id.
func getAuditEventHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		allowed, devScope := requireAuditAccess(role)
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		row, err := store.GetAuditEvent(r.Context(), id)
		if errors.Is(err, storage.ErrAuditEventNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		if devScope {
			// 404 (не 403) — не раскрываем существование чужих записей.
			userID, _ := currentUserID(r, store)
			if row.UserID != userID || row.EventType != "policy_tested" {
				http.NotFound(w, r)
				return
			}
		}
		writeJSON(w, http.StatusOK, rowToDetail(row))
	}
}

// auditExportRequestDTO — тело POST /api/audit-events/export. Filters
// зеркалят AuditFilter из storage; применяются к экспорту (закрывает
// MAJOR-1 финального ревью Итерации 9 — раньше filters записывались в
// audit-event но игнорировались, что было compliance-ловушкой).
type auditExportRequestDTO struct {
	Format         string              `json:"format"`
	IncludePayload *bool               `json:"include_payload"`
	Filters        *exportFiltersDTO   `json:"filters"`
}

type exportFiltersDTO struct {
	From            *time.Time `json:"from"`
	To              *time.Time `json:"to"`
	UserID          *string    `json:"user_id"`
	EventTypes      []string   `json:"event_types"`
	PolicyDecisions []string   `json:"policy_decisions"`
	RiskLevels      []string   `json:"risk_levels"`
	ModelProviderID *string    `json:"model_provider_id"`
	HasLeak         *bool      `json:"has_leak"`
	Q               *string    `json:"q"`
}

// toStorageFilter переводит ExportFiltersDTO в storage.AuditFilter.
// Limit=200 — MVP-ограничение размера экспорта (без cursor-разбивки).
func (e *exportFiltersDTO) toStorageFilter() storage.AuditFilter {
	f := storage.AuditFilter{Limit: 200}
	if e == nil {
		return f
	}
	f.From = e.From
	f.To = e.To
	f.UserID = e.UserID
	f.EventTypes = e.EventTypes
	f.PolicyDecisions = e.PolicyDecisions
	f.RiskLevels = e.RiskLevels
	f.ModelProviderID = e.ModelProviderID
	f.HasLeak = e.HasLeak
	f.Q = e.Q
	return f
}

func exportAuditEventsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		allowed, _ := requireAuditAccess(role)
		if !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var dto auditExportRequestDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if dto.Format != "csv" && dto.Format != "ndjson" {
			http.Error(w, "format: csv|ndjson", http.StatusBadRequest)
			return
		}
		includePayload := true
		if dto.IncludePayload != nil {
			includePayload = *dto.IncludePayload
		}
		filter := dto.Filters.toStorageFilter()

		// Audit-event перед стримингом — записывает фактический фильтр,
		// что устраняет расхождение «записано / применено» (MAJOR-1).
		userID, _ := currentUserID(r, store)
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "audit_exported",
			Detail: map[string]any{
				"format":          dto.Format,
				"include_payload": includePayload,
				"filters":         dto.Filters,
			},
		})

		rows, err := store.ListAuditEvents(r.Context(), filter)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		writeExport(w, dto.Format, includePayload, rows)
	}
}

func writeExport(
	w http.ResponseWriter, format string, includePayload bool,
	rows []storage.AuditEventRow,
) {
	ts := time.Now().UTC().Format("20060102-150405")
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition",
			`attachment; filename="audit-export-`+ts+`.csv"`)
		writeCSVExport(w, rows, includePayload)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition",
		`attachment; filename="audit-export-`+ts+`.ndjson"`)
	enc := json.NewEncoder(w)
	for _, r := range rows {
		row := rowToDetail(r)
		if !includePayload {
			row.MaskedPayload = nil
		}
		_ = enc.Encode(row)
	}
}

func writeCSVExport(
	w http.ResponseWriter, rows []storage.AuditEventRow, includePayload bool,
) {
	cols := []string{
		"id", "created_at", "user_id", "event_type", "policy_decision",
		"risk_level", "has_leak", "request_id",
	}
	if includePayload {
		cols = append(cols, "masked_payload")
	}
	_, _ = w.Write([]byte(strings.Join(cols, ",") + "\n"))
	for _, r := range rows {
		s := rowToSummary(r)
		fields := []string{
			s.ID,
			s.CreatedAt.UTC().Format(time.RFC3339Nano),
			s.UserID,
			s.EventType,
			derefOrEmpty(s.PolicyDecision),
			derefOrEmpty(s.RiskLevel),
			strconv.FormatBool(s.HasLeak),
			derefOrEmpty(s.RequestID),
		}
		if includePayload {
			fields = append(fields, derefOrEmpty(r.MaskedPayload))
		}
		_, _ = w.Write([]byte(strings.Join(escapeCSV(fields), ",") + "\n"))
	}
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func escapeCSV(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		if strings.ContainsAny(f, ",\"\n") {
			out[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
		} else {
			out[i] = f
		}
	}
	return out
}

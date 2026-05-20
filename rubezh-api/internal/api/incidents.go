package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// Role-доступ к incidents-эндпойнтам (план iteration-9.md §Р4).
var incidentReadRoles = map[auth.Role]bool{
	auth.RoleSecurityOfficer:   true,
	auth.RoleComplianceOfficer: true,
	auth.RoleAuditor:           true,
	auth.RoleAdmin:             true,
}

// incidentWriteRoles — могут создавать manual и PATCH.
var incidentWriteRoles = map[auth.Role]bool{
	auth.RoleSecurityOfficer: true,
	auth.RoleAdmin:           true,
}

// incidentDTO — публичная форма Incident (соответствует
// incidents.schema.json#Incident). trigger — computed (заполняется
// при чтении из связанного audit_event).
type incidentDTO struct {
	ID           string     `json:"id"`
	AuditEventID *string    `json:"audit_event_id"`
	UserID       *string    `json:"user_id"`
	ReporterID   *string    `json:"reporter_id"`
	AssigneeID   *string    `json:"assignee_id"`
	Severity     string     `json:"severity"`
	Status       string     `json:"status"`
	Trigger      *string    `json:"trigger"`
	Title        string     `json:"title"`
	Summary      *string    `json:"summary"`
	Resolution   *string    `json:"resolution"`
	ClosedAt     *time.Time `json:"closed_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type incidentListDTO struct {
	Incidents  []incidentDTO `json:"incidents"`
	NextCursor *string       `json:"next_cursor"`
}

type incidentCreateDTO struct {
	AuditEventID string  `json:"audit_event_id"`
	Severity     string  `json:"severity"`
	Title        string  `json:"title"`
	Summary      *string `json:"summary"`
}

type incidentPatchDTO struct {
	Status        *string `json:"status"`
	Severity      *string `json:"severity"`
	AssigneeID    *string `json:"assignee_id"`
	AssigneeUnset *bool   `json:"assignee_unset"`
	Resolution    *string `json:"resolution"`
}

// incidentNoteDTO/incidentNoteListDTO/incidentNoteCreateDTO + handlers
// — вынесены в incidents_notes.go (бюджет файла ≤500 строк).

// resolveTrigger — computed-поле trigger из связанного audit_event'а.
// План §Р4: manual ⇒ "manual"; auto ⇒ из event_type+detail audit-события.
func resolveTrigger(
	ctx context.Context, inc storage.Incident, store *storage.Storage,
) *string {
	if inc.ReporterID != nil {
		s := "manual"
		return &s
	}
	if inc.AuditEventID == nil {
		return nil
	}
	row, err := store.GetAuditEvent(ctx, *inc.AuditEventID)
	if err != nil {
		return nil
	}
	switch row.EventType {
	case "chat_blocked":
		if row.PolicyDecision != nil && *row.PolicyDecision == "escalate" {
			s := "escalate"
			return &s
		}
		s := "deny"
		return &s
	case "chat_response":
		if row.HasLeak() {
			s := "response_leak_detected"
			return &s
		}
	}
	return nil
}

func incidentToDTO(inc storage.Incident, trigger *string) incidentDTO {
	return incidentDTO{
		ID: inc.ID, AuditEventID: inc.AuditEventID, UserID: inc.UserID,
		ReporterID: inc.ReporterID, AssigneeID: inc.AssigneeID,
		Severity: inc.Severity, Status: inc.Status, Trigger: trigger,
		Title: inc.Title, Summary: inc.Summary, Resolution: inc.Resolution,
		ClosedAt: inc.ClosedAt, CreatedAt: inc.CreatedAt, UpdatedAt: inc.UpdatedAt,
	}
}

// listIncidentsHandler — GET /api/incidents.
func listIncidentsHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !incidentReadRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		filter, err := parseIncidentFilter(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rows, err := store.ListIncidents(r.Context(), filter)
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK,
			assembleIncidentList(r.Context(), store, rows, filter.Limit))
	}
}

func parseIncidentFilter(r *http.Request) (storage.IncidentFilter, error) {
	q := r.URL.Query()
	f := storage.IncidentFilter{
		Statuses:   q["status"],
		Severities: q["severity"],
	}
	if v := q.Get("assignee_id"); v != "" {
		if !isUUID(v) {
			return f, errors.New("assignee_id: невалидный UUID")
		}
		f.AssigneeID = &v
	}
	if v := q.Get("has_reporter"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, errors.New("has_reporter: true|false")
		}
		f.HasReporter = &b
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return f, errors.New("from: невалидный RFC3339")
		}
		f.From = &t
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

func assembleIncidentList(
	ctx context.Context, store *storage.Storage,
	rows []storage.Incident, limit int,
) incidentListDTO {
	out := make([]incidentDTO, 0, len(rows))
	for _, inc := range rows {
		out = append(out, incidentToDTO(inc, resolveTrigger(ctx, inc, store)))
	}
	var next *string
	if len(rows) >= limit && len(rows) > 0 {
		last := rows[len(rows)-1]
		cur := encodeAuditCursor(auditCursor{
			CreatedAt: last.CreatedAt, ID: last.ID,
		})
		next = &cur
	}
	return incidentListDTO{Incidents: out, NextCursor: next}
}

// getIncidentHandler — GET /api/incidents/:id.
func getIncidentHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !incidentReadRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		inc, err := store.GetIncident(r.Context(), id)
		if errors.Is(err, storage.ErrIncidentNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "ошибка чтения", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK,
			incidentToDTO(inc, resolveTrigger(r.Context(), inc, store)))
	}
}

// createIncidentHandler — POST /api/incidents (manual).
func createIncidentHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !incidentWriteRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var dto incidentCreateDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if !isUUID(dto.AuditEventID) {
			http.Error(w, "audit_event_id: невалидный UUID",
				http.StatusBadRequest)
			return
		}
		if dto.Title == "" || len(dto.Title) > 200 {
			http.Error(w, "title: 1..200", http.StatusBadRequest)
			return
		}
		if !validIncidentSeverity(dto.Severity) {
			http.Error(w, "severity: low|medium|high|critical",
				http.StatusBadRequest)
			return
		}
		reporterID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "user not resolved",
				http.StatusInternalServerError)
			return
		}
		// Идемпотентность 60s.
		if existing, e := store.FindManualIncidentForReporter(
			r.Context(), dto.AuditEventID, reporterID); e == nil {
			writeJSON(w, http.StatusOK,
				incidentToDTO(existing, ptrStr("manual")))
			return
		}
		auditEventID := dto.AuditEventID
		inc, _, err := store.CreateManualIncident(r.Context(),
			storage.IncidentInput{
				AuditEventID: &auditEventID, ReporterID: &reporterID,
				Severity: dto.Severity, Status: "open",
				Title: dto.Title, Summary: dto.Summary,
			},
			storage.AuditEvent{
				UserID: reporterID, EventType: "incident_created_manual",
				Detail: map[string]any{"audit_event_id": auditEventID},
			})
		if err != nil {
			http.Error(w, "ошибка создания", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, incidentToDTO(inc, ptrStr("manual")))
	}
}

// patchIncidentHandler — PATCH /api/incidents/:id с If-Match (RFC 7232).
// Декомпозирован на parsePatchIfMatch + executeIncidentPatch (≤60 строк).
func patchIncidentHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, _ := auth.RoleFromContext(r.Context())
		if !incidentWriteRoles[role] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		id := chi.URLParam(r, "id")
		if !isUUID(id) {
			http.NotFound(w, r)
			return
		}
		expected, ok := parsePatchIfMatch(w, r)
		if !ok {
			return
		}
		var dto incidentPatchDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		actorID, err := currentUserID(r, store)
		if err != nil {
			http.Error(w, "user not resolved",
				http.StatusInternalServerError)
			return
		}
		executeIncidentPatch(w, r, store, id, expected, dto, actorID)
	}
}

// parsePatchIfMatch читает и валидирует If-Match header (RFC 7232).
// При отсутствии — 428; при невалидном формате — 400.
func parsePatchIfMatch(
	w http.ResponseWriter, r *http.Request,
) (time.Time, bool) {
	ifMatch := r.Header.Get("If-Match")
	if ifMatch == "" {
		http.Error(w, "If-Match header required (RFC 7232)",
			http.StatusPreconditionRequired)
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, ifMatch)
	if err != nil {
		http.Error(w, "If-Match: ожидался RFC3339Nano",
			http.StatusBadRequest)
		return time.Time{}, false
	}
	return t, true
}

// executeIncidentPatch — фактическое выполнение PATCH. Вынесено из
// handler'а, чтобы каждая функция была ≤60 строк (план §4).
func executeIncidentPatch(
	w http.ResponseWriter, r *http.Request, store *storage.Storage,
	id string, expected time.Time, dto incidentPatchDTO, actorID string,
) {
	patch, audits, err := buildIncidentPatch(dto, actorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	prev, err := store.GetIncident(r.Context(), id)
	if errors.Is(err, storage.ErrIncidentNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "ошибка чтения", http.StatusInternalServerError)
		return
	}
	audits = enrichAuditsWithFrom(audits, prev, patch)
	inc, _, err := store.PatchIncident(r.Context(), id, patch, expected, audits)
	if errors.Is(err, storage.ErrIncidentNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, storage.ErrIncidentConflict) {
		http.Error(w, "If-Match mismatch", http.StatusPreconditionFailed)
		return
	}
	if err != nil {
		http.Error(w, "ошибка обновления",
			http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK,
		incidentToDTO(inc, resolveTrigger(r.Context(), inc, store)))
}

// buildIncidentPatch строит storage.IncidentPatch и список audit-event'ов.
func buildIncidentPatch(
	dto incidentPatchDTO, actorID string,
) (storage.IncidentPatch, []storage.AuditEvent, error) {
	p := storage.IncidentPatch{}
	audits := []storage.AuditEvent{}
	if dto.Status != nil {
		if !validIncidentStatus(*dto.Status) {
			return p, audits, errors.New("status: некорректное значение")
		}
		p.Status = dto.Status
		audits = append(audits, storage.AuditEvent{
			UserID: actorID, EventType: "incident_status_changed"})
	}
	if dto.Severity != nil {
		if !validIncidentSeverity(*dto.Severity) {
			return p, audits, errors.New("severity: некорректное")
		}
		p.Severity = dto.Severity
		audits = append(audits, storage.AuditEvent{
			UserID: actorID, EventType: "incident_severity_changed"})
	}
	if dto.AssigneeUnset != nil && *dto.AssigneeUnset {
		if dto.AssigneeID != nil {
			return p, audits,
				errors.New("assignee_unset + assignee_id несовместимы")
		}
		p.AssigneeUnset = true
		audits = append(audits, storage.AuditEvent{
			UserID: actorID, EventType: "incident_assigned"})
	} else if dto.AssigneeID != nil {
		if !isUUID(*dto.AssigneeID) {
			return p, audits, errors.New("assignee_id: невалидный UUID")
		}
		p.AssigneeID = dto.AssigneeID
		audits = append(audits, storage.AuditEvent{
			UserID: actorID, EventType: "incident_assigned"})
	}
	if dto.Resolution != nil {
		p.Resolution = dto.Resolution
	}
	if dto.Status != nil &&
		(*dto.Status == "resolved" || *dto.Status == "false_positive") {
		if dto.Resolution == nil || *dto.Resolution == "" {
			return p, audits,
				errors.New("resolution: обязательно при resolved/false_positive")
		}
		audits = append(audits, storage.AuditEvent{
			UserID: actorID, EventType: "incident_resolved"})
	}
	return p, audits, nil
}

func enrichAuditsWithFrom(
	audits []storage.AuditEvent, prev storage.Incident,
	patch storage.IncidentPatch,
) []storage.AuditEvent {
	for i := range audits {
		if audits[i].Detail == nil {
			audits[i].Detail = map[string]any{}
		}
		switch audits[i].EventType {
		case "incident_status_changed":
			audits[i].Detail["from"] = prev.Status
			if patch.Status != nil {
				audits[i].Detail["to"] = *patch.Status
			}
		case "incident_severity_changed":
			audits[i].Detail["from"] = prev.Severity
			if patch.Severity != nil {
				audits[i].Detail["to"] = *patch.Severity
			}
		}
	}
	return audits
}

// addIncidentNoteHandler/listIncidentNotesHandler/noteToDTO —
// вынесены в incidents_notes.go (бюджет файла ≤500 строк).

func validIncidentSeverity(s string) bool {
	switch s {
	case "low", "medium", "high", "critical":
		return true
	}
	return false
}

func validIncidentStatus(s string) bool {
	switch s {
	case "open", "investigating", "resolved", "false_positive":
		return true
	}
	return false
}

func ptrStr(s string) *string { return &s }

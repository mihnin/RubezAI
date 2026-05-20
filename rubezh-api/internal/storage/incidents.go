package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Sentinel errors.
var (
	// ErrIncidentAutoDuplicate — partial unique index сработал:
	// для audit_event_id уже есть auto-инцидент (reporter_id IS NULL).
	// Race-safe; оркестратор интерпретирует как «инцидент уже есть, OK».
	ErrIncidentAutoDuplicate = errors.New(
		"storage: auto-инцидент для audit_event_id уже существует")

	// ErrIncidentConflict — оптимистическая блокировка If-Match не совпала
	// (concurrent edit). Handler возвращает HTTP 412 Precondition Failed.
	ErrIncidentConflict = errors.New(
		"storage: incident изменён другим пользователем (If-Match mismatch)")

	// ErrIncidentNotFound — incident с указанным id не существует.
	ErrIncidentNotFound = errors.New("storage: incident не найден")

	// ErrIncidentNoteTooLong — content > 2000 символов (БД CHECK).
	ErrIncidentNoteTooLong = errors.New(
		"storage: текст заметки 1..2000 символов")
)

// Incident — запись таблицы incidents (см. migrations 000006 + 000008).
// ReporterID = NULL ⇔ auto-инцидент; ReporterID != nil ⇔ manual.
// AuditEventID nullable в схеме, но MVP-handler всегда требует
// (см. iteration-9.md §Р4).
type Incident struct {
	ID            string
	AuditEventID  *string
	UserID        *string
	ReporterID    *string
	AssigneeID    *string
	Severity      string
	Status        string
	Title         string
	Summary       *string
	Resolution    *string
	ClosedAt      *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// IncidentInput — данные для создания incident'а.
// ReporterID = nil → auto; иначе manual.
type IncidentInput struct {
	AuditEventID *string
	UserID       *string
	ReporterID   *string
	AssigneeID   *string
	Severity     string
	Status       string
	Title        string
	Summary      *string
}

// IncidentNote — запись incident_notes (append-only).
type IncidentNote struct {
	ID         string
	IncidentID string
	AuthorID   string
	Content    string
	CreatedAt  time.Time
}

// IncidentNoteInput — данные для AddIncidentNote.
type IncidentNoteInput struct {
	IncidentID string
	AuthorID   string
	Content    string
}

// IncidentPatch — изменения для PatchIncident. Все поля опциональны;
// nil-указатель означает «не менять». Resolution = &""  тоже «не
// менять»; для очистки использовать &"" с одновременным изменением
// status (handler валидирует).
type IncidentPatch struct {
	Status     *string
	Severity   *string
	AssigneeID *string // указатель-на-указатель не нужен: NULL передаётся в Apply()
	Resolution *string
	// AssigneeUnset = true означает «снять assignee» (set NULL).
	// Это не совмещается с AssigneeID != nil; handler проверяет.
	AssigneeUnset bool
}

// IncidentFilter — фильтры для ListIncidents.
type IncidentFilter struct {
	Statuses    []string
	Severities  []string
	AssigneeID  *string // если задан — фильтруем по assignee
	HasReporter *bool   // nil = все; true = manual; false = auto
	From, To    *time.Time
	CursorCreatedAt *time.Time
	CursorID        *string
	Limit           int // 1..200, дефолт устанавливает handler
}

// CreateAutoIncident атомарно создаёт auto-инцидент и audit-event
// `incident_created_auto` (Tx3, план iteration-9.md §Р4 Atomic Tx3).
// При нарушении partial unique index — ErrIncidentAutoDuplicate.
func (s *Storage) CreateAutoIncident(
	ctx context.Context, inc IncidentInput, ev AuditEvent,
) (Incident, string, error) {
	if inc.ReporterID != nil {
		return Incident{}, "", fmt.Errorf(
			"storage: CreateAutoIncident вызван с ReporterID != nil")
	}
	return s.createIncidentWithAudit(ctx, inc, ev)
}

// CreateManualIncident атомарно создаёт manual-инцидент и audit-event
// `incident_created_manual`. ReporterID обязателен.
func (s *Storage) CreateManualIncident(
	ctx context.Context, inc IncidentInput, ev AuditEvent,
) (Incident, string, error) {
	if inc.ReporterID == nil {
		return Incident{}, "", fmt.Errorf(
			"storage: CreateManualIncident требует ReporterID")
	}
	return s.createIncidentWithAudit(ctx, inc, ev)
}

// createIncidentWithAudit — общая реализация Tx3 (INSERT incidents +
// INSERT audit_events в одной транзакции).
func (s *Storage) createIncidentWithAudit(
	ctx context.Context, inc IncidentInput, ev AuditEvent,
) (Incident, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Incident{}, "", fmt.Errorf("storage: Tx3 begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out, err := insertIncident(ctx, tx, inc)
	if err != nil {
		return Incident{}, "", err
	}
	auditID, err := insertAuditEvent(ctx, tx, ev)
	if err != nil {
		return Incident{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, "", fmt.Errorf("storage: Tx3 commit: %w", err)
	}
	return out, auditID, nil
}

// insertIncident вставляет строку в incidents в рамках tx.
// Различает 23505 (partial unique) → ErrIncidentAutoDuplicate.
func insertIncident(
	ctx context.Context, tx pgx.Tx, inc IncidentInput,
) (Incident, error) {
	if inc.Status == "" {
		inc.Status = "open"
	}
	if inc.Severity == "" {
		inc.Severity = "medium"
	}
	row := Incident{
		AuditEventID: inc.AuditEventID, UserID: inc.UserID,
		ReporterID: inc.ReporterID, AssigneeID: inc.AssigneeID,
		Severity: inc.Severity, Status: inc.Status,
		Title: inc.Title, Summary: inc.Summary,
	}
	err := tx.QueryRow(ctx,
		`INSERT INTO incidents
		   (audit_event_id, user_id, reporter_id, assignee_id,
		    severity, status, title, summary)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, created_at, updated_at`,
		inc.AuditEventID, inc.UserID, inc.ReporterID, inc.AssigneeID,
		inc.Severity, inc.Status, inc.Title, inc.Summary,
	).Scan(&row.ID, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
			pgErr.ConstraintName == "idx_incidents_one_auto_per_event" {
			return Incident{}, ErrIncidentAutoDuplicate
		}
		return Incident{}, fmt.Errorf("storage: insert incident: %w", err)
	}
	return row, nil
}

// GetIncident читает incident по id.
func (s *Storage) GetIncident(ctx context.Context, id string) (Incident, error) {
	var inc Incident
	err := s.pool.QueryRow(ctx,
		`SELECT id, audit_event_id, user_id, reporter_id, assignee_id,
		        severity, status, title, summary, resolution,
		        closed_at, created_at, updated_at
		 FROM incidents WHERE id = $1`, id,
	).Scan(&inc.ID, &inc.AuditEventID, &inc.UserID, &inc.ReporterID,
		&inc.AssigneeID, &inc.Severity, &inc.Status, &inc.Title,
		&inc.Summary, &inc.Resolution, &inc.ClosedAt,
		&inc.CreatedAt, &inc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, ErrIncidentNotFound
	}
	if err != nil {
		return Incident{}, fmt.Errorf("storage: get incident: %w", err)
	}
	return inc, nil
}

// ListIncidents возвращает инциденты по фильтру + keyset cursor.
// Сортировка: created_at DESC, id DESC (newest first).
// Cursor — row-comparison (created_at, id) < ($1, $2).
func (s *Storage) ListIncidents(
	ctx context.Context, f IncidentFilter,
) ([]Incident, error) {
	q := `SELECT id, audit_event_id, user_id, reporter_id, assignee_id,
	             severity, status, title, summary, resolution,
	             closed_at, created_at, updated_at
	      FROM incidents`
	args := []any{}
	conds := []string{}
	addCond := func(c string, val ...any) {
		args = append(args, val...)
		conds = append(conds, c)
	}
	if f.CursorCreatedAt != nil && f.CursorID != nil {
		addCond(fmt.Sprintf(
			"(created_at, id) < ($%d, $%d::uuid)",
			len(args)+1, len(args)+2), *f.CursorCreatedAt, *f.CursorID)
	}
	if len(f.Statuses) > 0 {
		addCond(fmt.Sprintf("status = ANY($%d)", len(args)+1), f.Statuses)
	}
	if len(f.Severities) > 0 {
		addCond(fmt.Sprintf("severity = ANY($%d)", len(args)+1), f.Severities)
	}
	if f.AssigneeID != nil {
		addCond(fmt.Sprintf("assignee_id = $%d", len(args)+1), *f.AssigneeID)
	}
	if f.HasReporter != nil {
		if *f.HasReporter {
			conds = append(conds, "reporter_id IS NOT NULL")
		} else {
			conds = append(conds, "reporter_id IS NULL")
		}
	}
	if f.From != nil {
		addCond(fmt.Sprintf("created_at >= $%d", len(args)+1), *f.From)
	}
	if f.To != nil {
		addCond(fmt.Sprintf("created_at <= $%d", len(args)+1), *f.To)
	}
	if len(conds) > 0 {
		q += " WHERE " + joinAnd(conds)
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit)
	q += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list incidents: %w", err)
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		if err := rows.Scan(&inc.ID, &inc.AuditEventID, &inc.UserID,
			&inc.ReporterID, &inc.AssigneeID, &inc.Severity, &inc.Status,
			&inc.Title, &inc.Summary, &inc.Resolution, &inc.ClosedAt,
			&inc.CreatedAt, &inc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan incident: %w", err)
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// joinAnd — собирает срез строк через " AND ".
func joinAnd(conds []string) string {
	out := ""
	for i, c := range conds {
		if i > 0 {
			out += " AND "
		}
		out += c
	}
	return out
}

// PatchIncident атомарно обновляет incident (с If-Match по updated_at)
// и пишет 1..N audit-event'ов в той же транзакции. 0 affected →
// ErrIncidentConflict (HTTP 412).
//
// auditEvents соответствуют изменённым полям (status_changed,
// severity_changed, assigned, resolved) — формируются handler'ом.
func (s *Storage) PatchIncident(
	ctx context.Context, id string, patch IncidentPatch,
	expectedUpdatedAt time.Time, audits []AuditEvent,
) (Incident, []string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Incident{}, nil, fmt.Errorf("storage: patch tx begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	updated, err := applyIncidentPatch(ctx, tx, id, patch, expectedUpdatedAt)
	if err != nil {
		return Incident{}, nil, err
	}
	auditIDs := make([]string, 0, len(audits))
	for _, ev := range audits {
		auditID, err := insertAuditEvent(ctx, tx, ev)
		if err != nil {
			return Incident{}, nil, err
		}
		auditIDs = append(auditIDs, auditID)
	}
	if err := tx.Commit(ctx); err != nil {
		return Incident{}, nil, fmt.Errorf("storage: patch commit: %w", err)
	}
	return updated, auditIDs, nil
}

// applyIncidentPatch выполняет UPDATE incidents с If-Match через
// `WHERE id = $ AND updated_at = $expected RETURNING ...`. 0 строк
// — ErrIncidentConflict.
func applyIncidentPatch(
	ctx context.Context, tx pgx.Tx, id string,
	p IncidentPatch, expected time.Time,
) (Incident, error) {
	sets := []string{"updated_at = now()"}
	args := []any{id, expected}
	addSet := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Status != nil {
		addSet("status", *p.Status)
	}
	if p.Severity != nil {
		addSet("severity", *p.Severity)
	}
	if p.AssigneeUnset {
		sets = append(sets, "assignee_id = NULL")
	} else if p.AssigneeID != nil {
		addSet("assignee_id", *p.AssigneeID)
	}
	if p.Resolution != nil {
		addSet("resolution", *p.Resolution)
	}
	q := fmt.Sprintf(`UPDATE incidents SET %s
	                  WHERE id = $1 AND updated_at = $2
	                  RETURNING id, audit_event_id, user_id, reporter_id,
	                           assignee_id, severity, status, title, summary,
	                           resolution, closed_at, created_at, updated_at`,
		joinSets(sets))

	var inc Incident
	err := tx.QueryRow(ctx, q, args...).Scan(
		&inc.ID, &inc.AuditEventID, &inc.UserID, &inc.ReporterID,
		&inc.AssigneeID, &inc.Severity, &inc.Status, &inc.Title,
		&inc.Summary, &inc.Resolution, &inc.ClosedAt,
		&inc.CreatedAt, &inc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Различаем «нет такого id» от «If-Match mismatch».
		var exists bool
		if e := tx.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM incidents WHERE id = $1)`,
			id).Scan(&exists); e != nil {
			return Incident{}, fmt.Errorf("storage: проверка существования: %w", e)
		}
		if !exists {
			return Incident{}, ErrIncidentNotFound
		}
		return Incident{}, ErrIncidentConflict
	}
	if err != nil {
		return Incident{}, fmt.Errorf("storage: patch incident: %w", err)
	}
	return inc, nil
}

// joinSets — собирает SET-выражения через ", ".
func joinSets(sets []string) string {
	out := ""
	for i, s := range sets {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}

// AddIncidentNote добавляет append-only заметку. БД-триггер блокирует
// UPDATE/DELETE этой таблицы — заметки неизменяемые (forensics).
func (s *Storage) AddIncidentNote(
	ctx context.Context, in IncidentNoteInput,
) (IncidentNote, error) {
	if l := len(in.Content); l < 1 || l > 2000 {
		return IncidentNote{}, ErrIncidentNoteTooLong
	}
	note := IncidentNote{
		IncidentID: in.IncidentID, AuthorID: in.AuthorID, Content: in.Content,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO incident_notes (incident_id, author_id, content)
		 VALUES ($1, $2, $3) RETURNING id, created_at`,
		in.IncidentID, in.AuthorID, in.Content,
	).Scan(&note.ID, &note.CreatedAt)
	if err != nil {
		return IncidentNote{}, fmt.Errorf("storage: add note: %w", err)
	}
	return note, nil
}

// ListIncidentNotes возвращает заметки инцидента в порядке времени
// (хронология расследования).
func (s *Storage) ListIncidentNotes(
	ctx context.Context, incidentID string,
) ([]IncidentNote, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, incident_id, author_id, content, created_at
		 FROM incident_notes WHERE incident_id = $1
		 ORDER BY created_at ASC, id ASC`,
		incidentID)
	if err != nil {
		return nil, fmt.Errorf("storage: list notes: %w", err)
	}
	defer rows.Close()
	var out []IncidentNote
	for rows.Next() {
		var n IncidentNote
		if err := rows.Scan(&n.ID, &n.IncidentID, &n.AuthorID,
			&n.Content, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: scan note: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// FindManualIncidentForReporter ищет manual-инцидент для пары
// (audit_event_id, reporter_id), созданный за последние 60 секунд.
// Используется handler'ом для упрощённой идемпотентности (план §Р4):
// если уже существует — возвращаем его, не создавая дубль.
func (s *Storage) FindManualIncidentForReporter(
	ctx context.Context, auditEventID, reporterID string,
) (Incident, error) {
	var inc Incident
	err := s.pool.QueryRow(ctx,
		`SELECT id, audit_event_id, user_id, reporter_id, assignee_id,
		        severity, status, title, summary, resolution,
		        closed_at, created_at, updated_at
		 FROM incidents
		 WHERE audit_event_id = $1 AND reporter_id = $2
		   AND created_at > now() - interval '60 seconds'
		 ORDER BY created_at DESC LIMIT 1`,
		auditEventID, reporterID,
	).Scan(&inc.ID, &inc.AuditEventID, &inc.UserID, &inc.ReporterID,
		&inc.AssigneeID, &inc.Severity, &inc.Status, &inc.Title,
		&inc.Summary, &inc.Resolution, &inc.ClosedAt,
		&inc.CreatedAt, &inc.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Incident{}, ErrIncidentNotFound
	}
	if err != nil {
		return Incident{}, fmt.Errorf("storage: find manual: %w", err)
	}
	return inc, nil
}

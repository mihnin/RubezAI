package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// seedAuditEvent создаёт минимальную audit-запись и возвращает её id.
// Нужна для FK incidents.audit_event_id.
func seedAuditEvent(t *testing.T, s *Storage, eventType string) string {
	t.Helper()
	ctx := context.Background()
	userID, err := s.UserIDForRole(ctx, "user")
	if err != nil {
		t.Fatalf("UserIDForRole: %v", err)
	}
	id, err := s.InsertAuditEvent(ctx, AuditEvent{
		UserID: userID, EventType: eventType,
		Detail: map[string]any{"seeded_for_test": true},
	})
	if err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}
	return id
}

func TestCreateAutoIncidentRoundTrip(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	auditID := seedAuditEvent(t, s, "chat_blocked")
	userID, _ := s.UserIDForRole(ctx, "user")

	inc, evID, err := s.CreateAutoIncident(ctx,
		IncidentInput{
			AuditEventID: &auditID, UserID: &userID, ReporterID: nil,
			Severity: "high", Status: "open",
			Title: "Auto deny", Summary: ptr("test"),
		},
		AuditEvent{
			UserID: userID, EventType: "incident_created_auto",
			Detail: map[string]any{"audit_event_id": auditID, "trigger": "deny"},
		},
	)
	if err != nil {
		t.Fatalf("CreateAutoIncident: %v", err)
	}
	if inc.ID == "" || inc.ReporterID != nil || evID == "" {
		t.Errorf("неожиданный результат: inc=%+v ev=%s", inc, evID)
	}
	if inc.Severity != "high" || inc.Status != "open" {
		t.Errorf("severity/status некорректны: %+v", inc)
	}

	// Audit-event записан в той же транзакции — должен быть доступен.
	// Чтение через прямой SQL (audit нет специального Get'а пока).
	var found bool
	row := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM audit_events WHERE id = $1
		               AND event_type = 'incident_created_auto')`, evID)
	if err := row.Scan(&found); err != nil || !found {
		t.Errorf("audit-event incident_created_auto не записан: %v", err)
	}
}

func TestCreateAutoIncidentDuplicateReturnsSentinel(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	auditID := seedAuditEvent(t, s, "chat_blocked")
	userID, _ := s.UserIDForRole(ctx, "user")

	inp := IncidentInput{
		AuditEventID: &auditID, UserID: &userID,
		Severity: "high", Status: "open", Title: "first",
	}
	ev := AuditEvent{UserID: userID, EventType: "incident_created_auto"}

	if _, _, err := s.CreateAutoIncident(ctx, inp, ev); err != nil {
		t.Fatalf("первое создание: %v", err)
	}
	// Второе создание для того же audit_event_id с reporter_id IS NULL
	// должно дать sentinel.
	_, _, err := s.CreateAutoIncident(ctx, inp, ev)
	if !errors.Is(err, ErrIncidentAutoDuplicate) {
		t.Errorf("ожидалась ErrIncidentAutoDuplicate, получено: %v", err)
	}
}

func TestCreateManualIncidentNeedsReporter(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	auditID := seedAuditEvent(t, s, "chat_response")
	userID, _ := s.UserIDForRole(ctx, "user")
	repID, _ := s.UserIDForRole(ctx, "security_officer")

	// Без ReporterID — отказ
	_, _, err := s.CreateManualIncident(ctx,
		IncidentInput{AuditEventID: &auditID, UserID: &userID,
			Severity: "medium", Status: "open", Title: "x"},
		AuditEvent{UserID: repID, EventType: "incident_created_manual"})
	if err == nil {
		t.Error("CreateManualIncident без ReporterID должен падать")
	}

	// С ReporterID — успех; auto-инцидент с тем же audit_event допустим
	// (partial unique не срабатывает: reporter_id IS NOT NULL).
	inc, _, err := s.CreateManualIncident(ctx,
		IncidentInput{AuditEventID: &auditID, UserID: &userID,
			ReporterID: &repID,
			Severity: "medium", Status: "open", Title: "manual"},
		AuditEvent{UserID: repID, EventType: "incident_created_manual"})
	if err != nil {
		t.Fatalf("CreateManualIncident: %v", err)
	}
	if inc.ReporterID == nil || *inc.ReporterID != repID {
		t.Errorf("reporter_id некорректен: %+v", inc.ReporterID)
	}
}

func TestGetIncidentNotFound(t *testing.T) {
	s := withTestStorage(t)
	_, err := s.GetIncident(context.Background(),
		"00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrIncidentNotFound) {
		t.Errorf("ожидалась ErrIncidentNotFound, получено: %v", err)
	}
}

func TestPatchIncidentSetsClosedAtOnResolved(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	inc := createTestIncident(t, s, "open")
	if inc.ClosedAt != nil {
		t.Fatalf("новый incident со status=open не должен иметь closed_at")
	}

	resolution := "fixed by patch X"
	patched, _, err := s.PatchIncident(ctx, inc.ID,
		IncidentPatch{Status: ptr("resolved"), Resolution: &resolution},
		inc.UpdatedAt,
		[]AuditEvent{{UserID: *inc.UserID,
			EventType: "incident_status_changed",
			Detail:    map[string]any{"from": "open", "to": "resolved"}}})
	if err != nil {
		t.Fatalf("PatchIncident: %v", err)
	}
	if patched.Status != "resolved" {
		t.Errorf("status = %q, ожидалось resolved", patched.Status)
	}
	if patched.ClosedAt == nil {
		t.Error("триггер не выставил closed_at при resolved")
	}
}

func TestPatchIncidentIfMatchMismatchReturnsConflict(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	inc := createTestIncident(t, s, "open")

	// Первый PATCH меняет updated_at.
	_, _, err := s.PatchIncident(ctx, inc.ID,
		IncidentPatch{Status: ptr("investigating")},
		inc.UpdatedAt, nil)
	if err != nil {
		t.Fatalf("первый PATCH: %v", err)
	}

	// Второй PATCH с старым updated_at — конфликт.
	_, _, err = s.PatchIncident(ctx, inc.ID,
		IncidentPatch{Status: ptr("resolved"), Resolution: ptr("late")},
		inc.UpdatedAt, nil)
	if !errors.Is(err, ErrIncidentConflict) {
		t.Errorf("ожидалась ErrIncidentConflict, получено: %v", err)
	}
}

func TestPatchIncidentNotFound(t *testing.T) {
	s := withTestStorage(t)
	_, _, err := s.PatchIncident(context.Background(),
		"00000000-0000-0000-0000-000000000000",
		IncidentPatch{Status: ptr("resolved")},
		time.Now(), nil)
	if !errors.Is(err, ErrIncidentNotFound) {
		t.Errorf("ожидалась ErrIncidentNotFound, получено: %v", err)
	}
}

func TestPatchIncidentAuditEventsAtomic(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	inc := createTestIncident(t, s, "open")
	userID, _ := s.UserIDForRole(ctx, "security_officer")
	audits := []AuditEvent{
		{UserID: userID, EventType: "incident_status_changed",
			Detail: map[string]any{"from": "open", "to": "investigating"}},
		{UserID: userID, EventType: "incident_severity_changed",
			Detail: map[string]any{"from": "high", "to": "critical"}},
	}

	_, ids, err := s.PatchIncident(ctx, inc.ID,
		IncidentPatch{Status: ptr("investigating"), Severity: ptr("critical")},
		inc.UpdatedAt, audits)
	if err != nil {
		t.Fatalf("PatchIncident: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ожидалось 2 audit_event, получено %d", len(ids))
	}
	for _, evID := range ids {
		var found bool
		_ = s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM audit_events WHERE id = $1)`,
			evID).Scan(&found)
		if !found {
			t.Errorf("audit_event %s не найден", evID)
		}
	}
}

func TestListIncidentsFiltersAndCursor(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	// Создаём 3 incident'а с разными статусами.
	for _, st := range []string{"open", "investigating", "open"} {
		createTestIncidentStatus(t, s, st, "medium")
	}

	all, err := s.ListIncidents(ctx, IncidentFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("ожидалось ≥3 инцидента, получено %d", len(all))
	}

	onlyOpen, err := s.ListIncidents(ctx, IncidentFilter{
		Statuses: []string{"open"}})
	if err != nil {
		t.Fatalf("List status=open: %v", err)
	}
	for _, inc := range onlyOpen {
		if inc.Status != "open" {
			t.Errorf("фильтр не сработал: %+v", inc)
		}
	}

	// Cursor: получить page-2 (limit=1, cursor=первый).
	page, err := s.ListIncidents(ctx, IncidentFilter{Limit: 1})
	if err != nil || len(page) != 1 {
		t.Fatalf("page1: err=%v len=%d", err, len(page))
	}
	first := page[0]
	page2, err := s.ListIncidents(ctx, IncidentFilter{
		Limit:           1,
		CursorCreatedAt: &first.CreatedAt,
		CursorID:        &first.ID,
	})
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2: err=%v len=%d", err, len(page2))
	}
	if page2[0].ID == first.ID {
		t.Error("cursor не сдвинул выборку")
	}
}

func TestAddIncidentNoteAppendOnly(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	inc := createTestIncident(t, s, "open")
	userID, _ := s.UserIDForRole(ctx, "security_officer")

	note, err := s.AddIncidentNote(ctx, IncidentNoteInput{
		IncidentID: inc.ID, AuthorID: userID, Content: "первая заметка"})
	if err != nil {
		t.Fatalf("AddIncidentNote: %v", err)
	}

	// UPDATE заблокирован триггером.
	_, err = s.pool.Exec(ctx,
		`UPDATE incident_notes SET content = 'tampered' WHERE id = $1`,
		note.ID)
	if err == nil {
		t.Error("UPDATE incident_notes должен быть заблокирован")
	}
	// DELETE заблокирован.
	_, err = s.pool.Exec(ctx,
		`DELETE FROM incident_notes WHERE id = $1`, note.ID)
	if err == nil {
		t.Error("DELETE incident_notes должен быть заблокирован")
	}
}

func TestAddIncidentNoteRejectsLongContent(t *testing.T) {
	s := withTestStorage(t)
	inc := createTestIncident(t, s, "open")
	userID, _ := s.UserIDForRole(context.Background(), "security_officer")

	_, err := s.AddIncidentNote(context.Background(), IncidentNoteInput{
		IncidentID: inc.ID, AuthorID: userID,
		Content: strings.Repeat("x", 2001)})
	if !errors.Is(err, ErrIncidentNoteTooLong) {
		t.Errorf("ожидалась ErrIncidentNoteTooLong, получено: %v", err)
	}
	_, err = s.AddIncidentNote(context.Background(), IncidentNoteInput{
		IncidentID: inc.ID, AuthorID: userID, Content: ""})
	if !errors.Is(err, ErrIncidentNoteTooLong) {
		t.Errorf("пустая заметка должна отвергаться")
	}
}

func TestListIncidentNotesOrderByCreatedAt(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	inc := createTestIncident(t, s, "open")
	userID, _ := s.UserIDForRole(ctx, "security_officer")

	for _, txt := range []string{"первая", "вторая", "третья"} {
		_, err := s.AddIncidentNote(ctx, IncidentNoteInput{
			IncidentID: inc.ID, AuthorID: userID, Content: txt})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		// Минимальный sleep для различения created_at (timestamptz µs).
		time.Sleep(2 * time.Millisecond)
	}
	notes, err := s.ListIncidentNotes(ctx, inc.ID)
	if err != nil {
		t.Fatalf("ListIncidentNotes: %v", err)
	}
	if len(notes) != 3 {
		t.Fatalf("получено %d заметок, ожидалось 3", len(notes))
	}
	if notes[0].Content != "первая" || notes[2].Content != "третья" {
		t.Errorf("порядок неверный: %v", notes)
	}
}

func TestFindManualIncidentForReporter(t *testing.T) {
	s := withTestStorage(t)
	ctx := context.Background()
	auditID := seedAuditEvent(t, s, "chat_response")
	userID, _ := s.UserIDForRole(ctx, "user")
	repID, _ := s.UserIDForRole(ctx, "security_officer")

	inc, _, err := s.CreateManualIncident(ctx,
		IncidentInput{AuditEventID: &auditID, UserID: &userID,
			ReporterID: &repID, Severity: "medium",
			Status: "open", Title: "manual1"},
		AuditEvent{UserID: repID, EventType: "incident_created_manual"})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}

	found, err := s.FindManualIncidentForReporter(ctx, auditID, repID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.ID != inc.ID {
		t.Errorf("найден другой инцидент: %s != %s", found.ID, inc.ID)
	}

	// Другой reporter — не должен находить.
	otherRep, _ := s.UserIDForRole(ctx, "admin")
	_, err = s.FindManualIncidentForReporter(ctx, auditID, otherRep)
	if !errors.Is(err, ErrIncidentNotFound) {
		t.Errorf("чужой reporter должен дать ErrIncidentNotFound, получено: %v", err)
	}
}

// --- helpers ---

// ptr — указатель на переданное значение.
func ptr[T any](v T) *T { return &v }

// createTestIncident создаёт auto-инцидент со status='open' (severity=high)
// и возвращает его. Удобно для тестов PATCH/Notes/List.
func createTestIncident(t *testing.T, s *Storage, status string) Incident {
	t.Helper()
	return createTestIncidentStatus(t, s, status, "high")
}

func createTestIncidentStatus(
	t *testing.T, s *Storage, status, severity string,
) Incident {
	t.Helper()
	ctx := context.Background()
	auditID := seedAuditEvent(t, s, "chat_blocked")
	userID, _ := s.UserIDForRole(ctx, "user")
	inc, _, err := s.CreateAutoIncident(ctx,
		IncidentInput{AuditEventID: &auditID, UserID: &userID,
			Severity: severity, Status: status, Title: "test"},
		AuditEvent{UserID: userID, EventType: "incident_created_auto"})
	if err != nil {
		t.Fatalf("createTestIncident: %v", err)
	}
	return inc
}

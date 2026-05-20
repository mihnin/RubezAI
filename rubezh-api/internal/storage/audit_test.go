package storage

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

// devUserID возвращает id dev-пользователя роли user (хелпер для тестов).
func devUserID(t *testing.T, store *Storage) string {
	t.Helper()
	id, err := store.UserIDForRole(context.Background(), "user")
	if err != nil {
		t.Fatalf("UserIDForRole(user): %v", err)
	}
	return id
}

func TestInsertAuditEvent(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	marker := "audit-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	id, err := store.InsertAuditEvent(ctx, AuditEvent{
		UserID:    devUserID(t, store),
		EventType: "chat_error",
		Detail:    map[string]any{"marker": marker},
	})
	if err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}
	if id == "" {
		t.Fatal("id не присвоен")
	}
	var eventType, detailMarker string
	if err := store.Pool().QueryRow(ctx,
		`SELECT event_type, detail->>'marker' FROM audit_events WHERE id = $1`, id,
	).Scan(&eventType, &detailMarker); err != nil {
		t.Fatalf("чтение события: %v", err)
	}
	if eventType != "chat_error" {
		t.Errorf("event_type = %q", eventType)
	}
	if detailMarker != marker {
		t.Errorf("detail.marker = %q, ожидалось %q", detailMarker, marker)
	}
}

func TestAuditEventsAppendOnly(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	id, err := store.InsertAuditEvent(ctx, AuditEvent{
		UserID:    devUserID(t, store),
		EventType: "chat_request",
	})
	if err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}
	if _, err := store.Pool().Exec(ctx,
		`UPDATE audit_events SET event_type = 'tampered' WHERE id = $1`, id,
	); err == nil {
		t.Error("UPDATE audit_events должен отклоняться триггером append-only")
	}
	if _, err := store.Pool().Exec(ctx,
		`DELETE FROM audit_events WHERE id = $1`, id,
	); err == nil {
		t.Error("DELETE audit_events должен отклоняться триггером append-only")
	}
}

func TestInsertAuditEventFullFields(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	decision := "allow_masked"
	rule := "external-sensitive-masked"
	level := "medium"
	payload := "санированный текст"
	id, err := store.InsertAuditEvent(ctx, AuditEvent{
		UserID:         devUserID(t, store),
		EventType:      "chat_response",
		RiskLevel:      &level,
		RiskClasses:    []string{"pii", "commercial"},
		PolicyDecision: &decision,
		MatchedRule:    &rule,
		MaskedPayload:  &payload,
		Detail:         map[string]any{"request_id": "r-1"},
	})
	if err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}
	var gotDecision, gotPayload, gotLevel string
	var gotClasses []string
	if err := store.Pool().QueryRow(ctx,
		`SELECT policy_decision, masked_payload, risk_level, risk_classes
		 FROM audit_events WHERE id = $1`, id,
	).Scan(&gotDecision, &gotPayload, &gotLevel, &gotClasses); err != nil {
		t.Fatalf("чтение события: %v", err)
	}
	if gotDecision != decision || gotPayload != payload || gotLevel != level {
		t.Errorf("поля сохранены некорректно: %q / %q / %q",
			gotDecision, gotPayload, gotLevel)
	}
	if len(gotClasses) != 2 {
		t.Errorf("risk_classes = %v, ожидалось 2 элемента", gotClasses)
	}
}

// TestGetAuditEventNotFound — несуществующий id → ErrAuditEventNotFound.
func TestGetAuditEventNotFound(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	_, err := s.GetAuditEvent(context.Background(),
		"00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrAuditEventNotFound) {
		t.Errorf("ожидалась ErrAuditEventNotFound, получено: %v", err)
	}
}

// TestGetAuditEventRoundTrip — insert + get возвращает все поля.
func TestGetAuditEventRoundTrip(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()
	level := "high"
	decision := "deny"
	rule := "external+secret"
	payload := "запрос с api_key=SECRET_001"
	id, err := s.InsertAuditEvent(ctx, AuditEvent{
		UserID: devUserID(t, s), EventType: "chat_blocked",
		RiskLevel: &level, RiskClasses: []string{"secret"},
		PolicyDecision: &decision, MatchedRule: &rule,
		MaskedPayload: &payload,
		Detail:        map[string]any{"request_id": "r-X"},
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := s.GetAuditEvent(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id || got.EventType != "chat_blocked" ||
		got.RiskLevel == nil || *got.RiskLevel != "high" ||
		got.PolicyDecision == nil || *got.PolicyDecision != "deny" {
		t.Errorf("Get вернул некорректную запись: %+v", got)
	}
}

// TestListAuditEventsByEventType — мультизначный фильтр event_type.
func TestListAuditEventsByEventType(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()
	marker := "list-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	userID := devUserID(t, s)
	for _, et := range []string{"chat_request", "chat_response", "chat_error"} {
		if _, err := s.InsertAuditEvent(ctx, AuditEvent{
			UserID: userID, EventType: et,
			Detail: map[string]any{"marker": marker},
		}); err != nil {
			t.Fatalf("Insert %s: %v", et, err)
		}
	}

	rows, err := s.ListAuditEvents(ctx, AuditFilter{
		EventTypes: []string{"chat_request", "chat_response"},
		Q:          ptrStr("`marker`:`" + marker + "`"), // не сматчится
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Поскольку Q-фильтр у нас по masked_payload (а мы не задавали его),
	// результат должен быть пустым. Перезапросим без Q.
	rows, err = s.ListAuditEvents(ctx, AuditFilter{
		EventTypes: []string{"chat_request", "chat_response"},
		Limit:      100,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, r := range rows {
		if r.EventType != "chat_request" && r.EventType != "chat_response" {
			t.Errorf("неожиданный event_type: %s", r.EventType)
		}
	}
}

// TestListAuditEventsHasLeakFilter — has_leak=true возвращает только
// записи с detail.response_leak_detected = true.
func TestListAuditEventsHasLeakFilter(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()
	userID := devUserID(t, s)

	leakID, err := s.InsertAuditEvent(ctx, AuditEvent{
		UserID: userID, EventType: "chat_response",
		Detail: map[string]any{"response_leak_detected": true,
			"leaked_pseudonyms": []string{"ФИО_001"}},
	})
	if err != nil {
		t.Fatalf("Insert leak: %v", err)
	}
	noLeakID, err := s.InsertAuditEvent(ctx, AuditEvent{
		UserID: userID, EventType: "chat_response",
		Detail: map[string]any{"response_leak_detected": false},
	})
	if err != nil {
		t.Fatalf("Insert no-leak: %v", err)
	}

	tr := true
	leakRows, err := s.ListAuditEvents(ctx, AuditFilter{
		HasLeak: &tr, Limit: 100})
	if err != nil {
		t.Fatalf("List leak: %v", err)
	}
	var foundLeak, foundNoLeak bool
	for _, r := range leakRows {
		if r.ID == leakID {
			foundLeak = true
			if !r.HasLeak() {
				t.Error("HasLeak() возвращает false для записи с утечкой")
			}
		}
		if r.ID == noLeakID {
			foundNoLeak = true
		}
	}
	if !foundLeak {
		t.Error("HasLeak=true не вернул запись с утечкой")
	}
	if foundNoLeak {
		t.Error("HasLeak=true вернул запись без утечки")
	}
}

// TestListAuditEventsCursorPagination — keyset стабилен между страницами.
func TestListAuditEventsCursorPagination(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	ctx := context.Background()
	userID := devUserID(t, s)
	// Создаём 5 событий — порядок гарантированно различный по created_at.
	for i := 0; i < 5; i++ {
		if _, err := s.InsertAuditEvent(ctx, AuditEvent{
			UserID: userID, EventType: "chat_request",
		}); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	// Page 1: limit 2.
	page1, err := s.ListAuditEvents(ctx, AuditFilter{
		EventTypes: []string{"chat_request"}, Limit: 2})
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1: err=%v len=%d", err, len(page1))
	}

	// Page 2: cursor от последней строки page1.
	last := page1[len(page1)-1]
	page2, err := s.ListAuditEvents(ctx, AuditFilter{
		EventTypes:      []string{"chat_request"},
		CursorCreatedAt: &last.CreatedAt,
		CursorID:        &last.ID,
		Limit:           2,
	})
	if err != nil || len(page2) == 0 {
		t.Fatalf("page2: err=%v len=%d", err, len(page2))
	}
	// Проверяем, что страницы не пересекаются.
	for _, r1 := range page1 {
		for _, r2 := range page2 {
			if r1.ID == r2.ID {
				t.Errorf("страницы пересекаются по id=%s", r1.ID)
			}
		}
	}
}

func ptrStr(s string) *string { return &s }

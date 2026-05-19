package storage

import (
	"context"
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

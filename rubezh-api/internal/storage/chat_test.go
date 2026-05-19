package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

// newSession создаёт сессию для dev-пользователя роли user.
func newSession(t *testing.T, store *Storage) (userID, sessionID string) {
	t.Helper()
	userID = devUserID(t, store)
	s, err := store.CreateChatSession(context.Background(), userID, nil)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	return userID, s.ID
}

// newModelProviderID создаёт mock-провайдера и возвращает его id.
func newModelProviderID(t *testing.T, store *Storage) string {
	t.Helper()
	name := "chat-prov-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	p, err := store.CreateModelProvider(context.Background(), ModelProvider{
		Name: name, TrustLevel: "trusted_local", Adapter: "mock",
	})
	if err != nil {
		t.Fatalf("CreateModelProvider: %v", err)
	}
	return p.ID
}

func TestCreateAndListChatSession(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	userID := devUserID(t, store)
	title := "Сессия " + strconv.FormatInt(time.Now().UnixNano(), 36)
	created, err := store.CreateChatSession(ctx, userID, &title)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	if created.ID == "" || created.UserID != userID {
		t.Errorf("сессия создана некорректно: %+v", created)
	}
	if created.Title == nil || *created.Title != title {
		t.Errorf("title = %v", created.Title)
	}

	sessions, err := store.ListChatSessions(ctx, userID)
	if err != nil {
		t.Fatalf("ListChatSessions: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("созданная сессия отсутствует в списке пользователя")
	}

	// сессия не видна dev-пользователю другой роли (другой user_id)
	otherID, err := store.UserIDForRole(ctx, "admin")
	if err != nil {
		t.Fatalf("UserIDForRole(admin): %v", err)
	}
	otherSessions, err := store.ListChatSessions(ctx, otherID)
	if err != nil {
		t.Fatalf("ListChatSessions(other): %v", err)
	}
	for _, s := range otherSessions {
		if s.ID == created.ID {
			t.Error("сессия видна постороннему пользователю")
		}
	}
}

func TestCreateChatSessionNilTitle(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	created, err := store.CreateChatSession(context.Background(),
		devUserID(t, store), nil)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	if created.Title != nil {
		t.Errorf("Title = %v, ожидалось nil", created.Title)
	}
}

func TestGetChatSession(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	got, err := store.GetChatSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	if got.ID != sessionID || got.UserID != userID {
		t.Errorf("сессия прочитана некорректно: %+v", got)
	}
}

func TestGetChatSessionNotFound(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	_, err := store.GetChatSession(context.Background(),
		"00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrChatSessionNotFound) {
		t.Errorf("несуществующая сессия: ожидалась ErrChatSessionNotFound, "+
			"получено %v", err)
	}
}

func TestRecordChatRequest(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	reqID := "req-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ids, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID:   sessionID,
		UserContent: "Звонил ТЕЛЕФОН_001",
		Sanitization: SanitizationData{
			RiskLevel:   "medium",
			RiskScore:   0.5,
			RiskClasses: []string{"pii"},
			Entities:    json.RawMessage(`[{"type":"PHONE"}]`),
		},
		Audit: AuditEvent{
			UserID:    userID,
			EventType: "chat_request",
			Detail:    map[string]any{"request_id": reqID},
		},
	})
	if err != nil {
		t.Fatalf("RecordChatRequest: %v", err)
	}
	if ids.UserMessageID == "" || ids.SanitizationResultID == "" ||
		ids.AuditEventID == "" {
		t.Fatalf("не все id присвоены: %+v", ids)
	}

	var role, content string
	if err := store.Pool().QueryRow(ctx,
		`SELECT role, content FROM chat_messages WHERE id = $1`,
		ids.UserMessageID,
	).Scan(&role, &content); err != nil {
		t.Fatalf("чтение сообщения: %v", err)
	}
	if role != "user" || content != "Звонил ТЕЛЕФОН_001" {
		t.Errorf("сообщение пользователя: role=%q content=%q", role, content)
	}

	var srMessageID, riskLevel string
	if err := store.Pool().QueryRow(ctx,
		`SELECT message_id, risk_level FROM sanitization_results WHERE id = $1`,
		ids.SanitizationResultID,
	).Scan(&srMessageID, &riskLevel); err != nil {
		t.Fatalf("чтение sanitization_result: %v", err)
	}
	if srMessageID != ids.UserMessageID || riskLevel != "medium" {
		t.Error("sanitization_result привязан некорректно")
	}

	var eventType string
	if err := store.Pool().QueryRow(ctx,
		`SELECT event_type FROM audit_events WHERE id = $1`, ids.AuditEventID,
	).Scan(&eventType); err != nil {
		t.Fatalf("чтение аудита: %v", err)
	}
	if eventType != "chat_request" {
		t.Errorf("event_type = %q", eventType)
	}
}

func TestRecordChatRequestIsAtomic(t *testing.T) {
	// при сбое (несуществующая сессия) ни одна из трёх записей не остаётся
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()

	reqID := "atomic-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	_, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID:   "00000000-0000-0000-0000-000000000000",
		UserContent: "x",
		Sanitization: SanitizationData{
			RiskLevel: "low", RiskScore: 0, RiskClasses: []string{},
			Entities: json.RawMessage(`[]`),
		},
		Audit: AuditEvent{
			UserID:    devUserID(t, store),
			EventType: "chat_request",
			Detail:    map[string]any{"request_id": reqID},
		},
	})
	if err == nil {
		t.Fatal("RecordChatRequest с несуществующей сессией должен дать ошибку")
	}
	var auditCount int
	if err := store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_events WHERE detail->>'request_id' = $1`,
		reqID,
	).Scan(&auditCount); err != nil {
		t.Fatalf("подсчёт аудита: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("аудит записан несмотря на откат транзакции (%d)", auditCount)
	}
}

func TestRecordChatTermination(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)
	providerID := newModelProviderID(t, store)

	ids, err := store.RecordChatTermination(ctx, ChatTerminationRecord{
		SessionID:        sessionID,
		AssistantContent: "[mock] ответ",
		ModelProviderID:  &providerID,
		Audit: AuditEvent{
			UserID: userID, EventType: "chat_response",
		},
	})
	if err != nil {
		t.Fatalf("RecordChatTermination: %v", err)
	}
	if ids.AssistantMessageID == "" || ids.AuditEventID == "" {
		t.Fatalf("id не присвоены: %+v", ids)
	}
	var role, content string
	if err := store.Pool().QueryRow(ctx,
		`SELECT role, content FROM chat_messages WHERE id = $1`,
		ids.AssistantMessageID,
	).Scan(&role, &content); err != nil {
		t.Fatalf("чтение сообщения ассистента: %v", err)
	}
	if role != "assistant" || content != "[mock] ответ" {
		t.Errorf("сообщение ассистента: role=%q content=%q", role, content)
	}
}

func TestRecordChatTerminationWithoutAssistant(t *testing.T) {
	// deny/escalate/ошибка без ответа LLM: пишется только аудит-событие
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	ids, err := store.RecordChatTermination(ctx, ChatTerminationRecord{
		SessionID:        sessionID,
		AssistantContent: "",
		Audit: AuditEvent{
			UserID: userID, EventType: "chat_blocked",
		},
	})
	if err != nil {
		t.Fatalf("RecordChatTermination: %v", err)
	}
	if ids.AssistantMessageID != "" {
		t.Errorf("сообщение ассистента не должно создаваться: %q",
			ids.AssistantMessageID)
	}
	if ids.AuditEventID == "" {
		t.Error("аудит-событие не записано")
	}
}

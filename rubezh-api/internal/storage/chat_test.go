package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
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

// TestRecordChatRequestWritesRequestIDAndMappings — Итерация 9:
// поле request_id и зашифрованные mappings пишутся в Tx1.
func TestRecordChatRequestWritesRequestIDAndMappings(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	reqID := "rid-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	mappings := []PseudonymMappingInput{
		{Pseudonym: "ФИО_001", EntityType: "PERSON",
			RawHash: "h1", RawValueEncrypted: []byte{0xAA, 0xBB}},
		{Pseudonym: "ТЕЛ_001", EntityType: "PHONE",
			RawHash: "h2", RawValueEncrypted: []byte{0xCC}},
	}
	ids, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID: sessionID, UserContent: "Звонил ФИО_001",
		RequestID: reqID,
		Sanitization: SanitizationData{
			RiskLevel: "medium", RiskScore: 0.5,
			RiskClasses: []string{"pii"},
			Entities: json.RawMessage(`[
				{"type":"PERSON","category":"pii","pseudonym":"ФИО_001","raw_hash":"h1","start":7,"end":15},
				{"type":"PHONE","category":"pii","pseudonym":"ТЕЛ_001","raw_hash":"h2","start":16,"end":28}
			]`),
		},
		Mappings: mappings,
		Audit: AuditEvent{
			UserID: userID, EventType: "chat_request",
			Detail: map[string]any{"request_id": reqID},
		},
	})
	if err != nil {
		t.Fatalf("RecordChatRequest: %v", err)
	}

	// 1. chat_messages.request_id заполнен.
	var gotReqID string
	if err := store.Pool().QueryRow(ctx,
		`SELECT request_id FROM chat_messages WHERE id = $1`,
		ids.UserMessageID).Scan(&gotReqID); err != nil {
		t.Fatalf("чтение request_id: %v", err)
	}
	if gotReqID != reqID {
		t.Errorf("chat_messages.request_id = %q, ожидалось %q", gotReqID, reqID)
	}

	// 2. pseudonym_mappings записаны и привязаны к sanitization_result.
	rows, err := store.ListPseudonymMappings(ctx, ids.SanitizationResultID)
	if err != nil {
		t.Fatalf("ListPseudonymMappings: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("ожидалось 2 mapping'а, получено %d", len(rows))
	}
}

// TestRecordChatRequestEmptyRequestIDIsNullable — backward compat
// для тестов Итерации 8 (без request_id).
func TestRecordChatRequestEmptyRequestIDIsNullable(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	ids, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID: sessionID, UserContent: "no req-id",
		// RequestID: "" — backward compat
		Sanitization: SanitizationData{RiskLevel: "low",
			RiskClasses: []string{}, Entities: json.RawMessage(`[]`)},
		Audit: AuditEvent{UserID: userID, EventType: "chat_request"},
	})
	if err != nil {
		t.Fatalf("RecordChatRequest: %v", err)
	}
	var reqID *string
	_ = store.Pool().QueryRow(ctx,
		`SELECT request_id FROM chat_messages WHERE id = $1`,
		ids.UserMessageID).Scan(&reqID)
	if reqID != nil {
		t.Errorf("request_id должен быть NULL, получено %q", *reqID)
	}
}

// TestRecordChatTerminationWritesRequestID — assistant получает тот же
// request_id что и user (план §Р6).
func TestRecordChatTerminationWritesRequestID(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)
	providerID := newModelProviderID(t, store)

	reqID := "pair-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID: sessionID, UserContent: "test", RequestID: reqID,
		Sanitization: SanitizationData{RiskLevel: "low",
			RiskClasses: []string{}, Entities: json.RawMessage(`[]`)},
		Audit: AuditEvent{UserID: userID, EventType: "chat_request"},
	}); err != nil {
		t.Fatalf("Tx1: %v", err)
	}
	ids, err := store.RecordChatTermination(ctx, ChatTerminationRecord{
		SessionID: sessionID, AssistantContent: "ответ",
		ModelProviderID: &providerID, RequestID: reqID,
		Audit: AuditEvent{UserID: userID, EventType: "chat_response"},
	})
	if err != nil {
		t.Fatalf("Tx2: %v", err)
	}

	var assistantReqID string
	_ = store.Pool().QueryRow(ctx,
		`SELECT request_id FROM chat_messages WHERE id = $1`,
		ids.AssistantMessageID).Scan(&assistantReqID)
	if assistantReqID != reqID {
		t.Errorf("assistant.request_id = %q, ожидалось %q", assistantReqID, reqID)
	}
}

// TestListChatMessagesRoundTrip — JOIN sanitization_results возвращает
// user-сообщение с summary, assistant — без.
func TestListChatMessagesRoundTrip(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)
	providerID := newModelProviderID(t, store)
	reqID := "list-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	_, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID: sessionID, UserContent: "Тестовый запрос с ФИО_001",
		RequestID: reqID,
		Sanitization: SanitizationData{
			RiskLevel: "medium", RiskScore: 0.42,
			RiskClasses: []string{"pii"},
			Entities: json.RawMessage(`[
				{"type":"PERSON","category":"pii","pseudonym":"ФИО_001","raw_hash":"hashX","start":15,"end":22}
			]`),
		},
		Audit: AuditEvent{UserID: userID, EventType: "chat_request"},
	})
	if err != nil {
		t.Fatalf("Tx1: %v", err)
	}
	_, err = store.RecordChatTermination(ctx, ChatTerminationRecord{
		SessionID: sessionID, AssistantContent: "Привет, ФИО_001",
		ModelProviderID: &providerID, RequestID: reqID,
		Audit: AuditEvent{UserID: userID, EventType: "chat_response"},
	})
	if err != nil {
		t.Fatalf("Tx2: %v", err)
	}

	msgs, err := store.ListChatMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("ожидалось 2 сообщения, получено %d", len(msgs))
	}
	// user → есть Sanitization; assistant → nil.
	var userMsg, assistantMsg *ChatMessageWithSummary
	for i := range msgs {
		switch msgs[i].Role {
		case "user":
			userMsg = &msgs[i]
		case "assistant":
			assistantMsg = &msgs[i]
		}
	}
	if userMsg == nil || assistantMsg == nil {
		t.Fatalf("не нашли user/assistant: %+v", msgs)
	}
	if userMsg.SanitizationSummary == nil {
		t.Fatal("у user-сообщения должна быть Sanitization")
	}
	if assistantMsg.SanitizationSummary != nil {
		t.Error("у assistant-сообщения Sanitization должен быть nil")
	}
	if userMsg.RequestID == nil || *userMsg.RequestID != reqID {
		t.Errorf("user.request_id = %v, ожидалось %q", userMsg.RequestID, reqID)
	}
	if userMsg.SanitizationSummary.Risk.Level != "medium" {
		t.Errorf("risk_level = %q", userMsg.SanitizationSummary.Risk.Level)
	}
}

// TestListChatMessagesEntitiesWhitelistFiltersStartEnd — критический
// инвариант безопасности (план §Р5): start/end не должны утекать в API.
func TestListChatMessagesEntitiesWhitelistFiltersStartEnd(t *testing.T) {
	store := testStore(t)
	defer store.Close()
	ctx := context.Background()
	userID, sessionID := newSession(t, store)

	// Намеренно записываем entities с start/end — должны быть отсеяны.
	if _, err := store.RecordChatRequest(ctx, ChatRequestRecord{
		SessionID: sessionID, UserContent: "x",
		Sanitization: SanitizationData{
			RiskLevel: "low", RiskClasses: []string{"pii"},
			Entities: json.RawMessage(`[
				{"type":"PERSON","category":"pii","pseudonym":"ФИО_001","raw_hash":"h","start":10,"end":15},
				{"type":"PHONE","category":"pii","pseudonym":"ТЕЛ_001","raw_hash":"h2","start":20,"end":30,"confidence":0.9}
			]`),
		},
		Audit: AuditEvent{UserID: userID, EventType: "chat_request"},
	}); err != nil {
		t.Fatalf("Tx1: %v", err)
	}

	msgs, err := store.ListChatMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].SanitizationSummary == nil {
		t.Fatal("ожидалось 1 сообщение с Sanitization")
	}
	entities := msgs[0].SanitizationSummary.Entities
	if len(entities) != 2 {
		t.Fatalf("ожидалось 2 entity, получено %d", len(entities))
	}
	// Сериализуем DTO в JSON — это то, что увидит API-handler.
	encoded, err := json.Marshal(entities)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, banned := range []string{`"start"`, `"end"`, `"confidence"`, `"detector"`} {
		if strings.Contains(string(encoded), banned) {
			t.Errorf("в JSON-выводе обнаружено запрещённое поле %s: %s",
				banned, string(encoded))
		}
	}
	// Поля whitelist должны быть.
	for _, want := range []string{`"type"`, `"category"`, `"pseudonym"`, `"raw_hash"`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("в JSON-выводе нет публичного поля %s: %s",
				want, string(encoded))
		}
	}
}

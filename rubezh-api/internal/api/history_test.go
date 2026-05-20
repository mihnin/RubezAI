package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// TestListChatMessagesOwnerSeesHistory — happy path: владелец сессии
// получает историю с sanitization_summary.
func TestListChatMessagesOwnerSeesHistory(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	ctx := context.Background()

	userID, _ := store.UserIDForRole(ctx, "user")
	session, err := store.CreateChatSession(ctx, userID, nil)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	// Засеять user-сообщение через RecordChatRequest с entities (включая start/end).
	_, err = store.RecordChatRequest(ctx, storage.ChatRequestRecord{
		SessionID: session.ID,
		UserContent: "Привет ФИО_001",
		RequestID: "r-history-1",
		Sanitization: storage.SanitizationData{
			RiskLevel: "medium", RiskScore: 0.5,
			RiskClasses: []string{"pii"},
			Entities: json.RawMessage(`[
				{"type":"PERSON","category":"pii","pseudonym":"ФИО_001","raw_hash":"hX","start":7,"end":14}
			]`),
		},
		Audit: storage.AuditEvent{
			UserID: userID, EventType: "chat_request",
		},
	})
	if err != nil {
		t.Fatalf("RecordChatRequest: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chat/sessions/"+session.ID+"/messages", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d (%s)", rec.Code, rec.Body)
	}

	// **Критический инвариант** (план §Р5): в ответе НЕТ ключей start/end.
	body := rec.Body.String()
	for _, banned := range []string{`"start"`, `"end"`} {
		if strings.Contains(body, banned) {
			t.Errorf("в JSON-ответе обнаружено запрещённое поле %s: %s",
				banned, body)
		}
	}
	// Проверяем, что есть нужные поля.
	for _, want := range []string{
		`"session_id":"` + session.ID,
		`"role":"user"`,
		`"request_id":"r-history-1"`,
		`"pseudonym":"ФИО_001"`,
		`"raw_hash":"hX"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("в ответе нет %s: %s", want, body)
		}
	}
}

func TestListChatMessagesNonOwner404(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	ctx := context.Background()
	// Создаём сессию от security_officer; запрашиваем под user.
	secID, _ := store.UserIDForRole(ctx, "security_officer")
	session, err := store.CreateChatSession(ctx, secID, nil)
	if err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet,
		"/api/chat/sessions/"+session.ID+"/messages", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("чужая сессия должна давать 404, получено %d", rec.Code)
	}
}

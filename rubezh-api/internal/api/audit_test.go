package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// fullTestRouter — полный HTTP-роутер с реальным storage (для тестов
// audit/incidents/history-эндпойнтов). Пропускается без TEST_DATABASE_URL.
func fullTestRouter(t *testing.T) (http.Handler, *storage.Storage, func()) {
	t.Helper()
	store, closeStore := dbStore(t)
	router := NewRouter(Deps{
		Logger:       discardLogger(),
		Store:        store,
		AuthSecret:   apiTestSecret,
		SanitizerURL: "http://disabled",
	})
	return router, store, closeStore
}

// roleToken — Bearer-токен с заданной ролью (helper для тестов).
func roleToken(role auth.Role) string {
	return "Bearer " + auth.IssueToken(role, apiTestSecret)
}

func seedAuditAPI(
	t *testing.T, store *storage.Storage, eventType string,
) string {
	t.Helper()
	userID, err := store.UserIDForRole(context.Background(), "user")
	if err != nil {
		t.Fatalf("UserIDForRole: %v", err)
	}
	id, err := store.InsertAuditEvent(context.Background(),
		storage.AuditEvent{UserID: userID, EventType: eventType,
			Detail: map[string]any{"marker": time.Now().UnixNano()}})
	if err != nil {
		t.Fatalf("InsertAuditEvent: %v", err)
	}
	return id
}

func TestListAuditEventsRequiresAuth(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet, "/api/audit-events", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestListAuditEventsForbiddenForUser(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/audit-events", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403", rec.Code)
	}
}

func TestListAuditEventsAllowedForSecurityOfficer(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/audit-events", nil)
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d (тело %s)", rec.Code, rec.Body)
	}
	var out auditEventListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
}

// TestGetAuditEventForDeveloperOutOfScope404 — критический тест
// безопасности (план §Р3): developer видит ТОЛЬКО свои policy_tested;
// чужие или не-policy_tested события дают 404, а не 403 (нераскрытие).
func TestGetAuditEventForDeveloperOutOfScope404(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	id := seedAuditAPI(t, store, "chat_request")

	req := httptest.NewRequest(http.MethodGet,
		"/api/audit-events/"+id, nil)
	req.Header.Set("Authorization", roleToken(auth.RoleDeveloper))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("developer должен видеть 404 для не-policy_tested: %d %s",
			rec.Code, rec.Body)
	}
}

func TestGetAuditEventForSecurityOfficerOK(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	id := seedAuditAPI(t, store, "chat_response")

	req := httptest.NewRequest(http.MethodGet,
		"/api/audit-events/"+id, nil)
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d (тело %s)", rec.Code, rec.Body)
	}
}

func TestExportAuditEventsCSV(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	body := `{"format":"csv","include_payload":false}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/audit-events/export", strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d (тело %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "audit-export-") {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

func TestAuditCursorRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	id := "abc-123"
	enc := encodeAuditCursor(auditCursor{CreatedAt: now, ID: id})
	dec, err := decodeAuditCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.ID != id || !dec.CreatedAt.Equal(now) {
		t.Errorf("round-trip искажение: %+v", dec)
	}
}

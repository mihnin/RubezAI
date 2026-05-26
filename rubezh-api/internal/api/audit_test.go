package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// fullTestRouter — полный HTTP-роутер с реальным storage (для тестов
// audit/incidents/history-эндпойнтов). Пропускается без TEST_DATABASE_URL.
func fullTestRouter(t *testing.T) (http.Handler, *storage.Storage, func()) {
	t.Helper()
	store, closeStore := dbStore(t)
	router, _ := NewRouter(Deps{
		Logger:       discardLogger(),
		Store:        store,
		AuthSecret:   apiTestSecret,
		SanitizerURL: "http://disabled",
		Embedder:     llm.MockEmbedder{},
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
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()

	// Seed: 2 события разных типов с уникальным маркером в
	// masked_payload, чтобы при include_payload=true маркер попал
	// в CSV-колонку (Техдолг 9-3).
	marker := "export-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	reqID := seedAuditWithMarker(t, store, "chat_request", marker)
	seedAuditWithMarker(t, store, "chat_blocked", marker)

	// Фильтруем только chat_request → в CSV ровно 1 строка с маркером.
	body := `{"format":"csv","include_payload":true,` +
		`"filters":{"event_types":["chat_request"]}}`
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

	// MAJOR-B ревью v2: проверить, что фильтр event_types РЕАЛЬНО
	// применён к выгрузке (не только записан в audit_exported.detail).
	// Если фильтр не применился, в CSV была бы строка chat_blocked.
	csv := rec.Body.String()
	if strings.Count(csv, "chat_blocked") > 0 {
		t.Errorf("CSV содержит chat_blocked, хотя фильтр event_types=chat_request: %s", csv)
	}
	// Минимум 2 строки (header + 1 event); строка chat_request с маркером.
	if !strings.Contains(csv, "chat_request") {
		t.Errorf("CSV не содержит chat_request: %s", csv)
	}

	// Техдолг 9-3: include_payload=true → masked_payload реально
	// выгружен. marker записан как masked_payload в seedAuditWithMarker;
	// проверяем что он есть в CSV.
	if !strings.Contains(csv, marker) {
		t.Errorf("CSV не содержит marker %q (include_payload=true не работает): %s",
			marker, csv)
	}
	// Bonus: id seed-события в колонке id CSV — отслеживаемая запись.
	if !strings.Contains(csv, reqID) {
		t.Errorf("CSV не содержит id %q seed-события: %s", reqID, csv)
	}

	// MINOR-10: audit_exported реально записан в БД (compliance-инвариант).
	rows, err := store.ListAuditEvents(context.Background(),
		storage.AuditFilter{
			EventTypes: []string{"audit_exported"}, Limit: 10,
		})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	found := false
	for _, r := range rows {
		var det map[string]any
		_ = json.Unmarshal(r.Detail, &det)
		if det["format"] == "csv" {
			found = true
			break
		}
	}
	if !found {
		t.Error("audit-event audit_exported (format=csv) не записан")
	}
}

// seedAuditWithMarker — вставляет audit-событие с конкретным event_type
// и marker в masked_payload (для проверки CSV-выгрузки) + detail.marker
// (для отлова в jsonb-фильтрах).
func seedAuditWithMarker(
	t *testing.T, store *storage.Storage, eventType, marker string,
) string {
	t.Helper()
	userID, _ := store.UserIDForRole(context.Background(), "user")
	payload := marker
	id, err := store.InsertAuditEvent(context.Background(),
		storage.AuditEvent{
			UserID: userID, EventType: eventType,
			MaskedPayload: &payload,
			Detail:        map[string]any{"marker": marker},
		})
	if err != nil {
		t.Fatalf("InsertAuditEvent %s: %v", eventType, err)
	}
	return id
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

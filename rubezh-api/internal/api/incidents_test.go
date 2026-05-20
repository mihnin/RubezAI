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

// seedIncident создаёт auto-инцидент через storage и возвращает его.
func seedIncident(t *testing.T, store *storage.Storage) storage.Incident {
	t.Helper()
	auditID := seedAuditAPI(t, store, "chat_blocked")
	userID, _ := store.UserIDForRole(context.Background(), "user")
	inc, _, err := store.CreateAutoIncident(context.Background(),
		storage.IncidentInput{
			AuditEventID: &auditID, UserID: &userID,
			Severity: "high", Status: "open",
			Title: "api-test " + time.Now().Format("150405.000"),
		},
		storage.AuditEvent{
			UserID: userID, EventType: "incident_created_auto",
		})
	if err != nil {
		t.Fatalf("seedIncident: %v", err)
	}
	return inc
}

func TestListIncidentsForbiddenForUser(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/incidents", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d", rec.Code)
	}
}

func TestListIncidentsAllowedForSecurityOfficer(t *testing.T) {
	router, _, closeStore := fullTestRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/incidents", nil)
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("code = %d (%s)", rec.Code, rec.Body)
	}
}

// TestPatchIncidentRequiresIfMatch — закрывает MINOR-8 ревью v2:
// без If-Match → 428 Precondition Required.
func TestPatchIncidentRequiresIfMatch(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	inc := seedIncident(t, store)

	body := `{"status":"investigating"}`
	req := httptest.NewRequest(http.MethodPatch,
		"/api/incidents/"+inc.ID, strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionRequired {
		t.Errorf("code = %d, ожидалось 428", rec.Code)
	}
}

// TestPatchIncidentConflict — Конкурентный edit → 412 Precondition Failed.
func TestPatchIncidentConflict(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	inc := seedIncident(t, store)
	// Меняем сам incident отдельным PATCH, чтобы старый updated_at не подходил.
	_, _, err := store.PatchIncident(context.Background(), inc.ID,
		storage.IncidentPatch{Status: ptrStr("investigating")},
		inc.UpdatedAt, nil)
	if err != nil {
		t.Fatalf("первый PATCH: %v", err)
	}

	body := `{"status":"resolved","resolution":"x"}`
	req := httptest.NewRequest(http.MethodPatch,
		"/api/incidents/"+inc.ID, strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", inc.UpdatedAt.Format(time.RFC3339Nano))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("code = %d, ожидалось 412 (%s)", rec.Code, rec.Body)
	}
}

func TestPatchIncidentResolvedRequiresResolution(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	inc := seedIncident(t, store)
	body := `{"status":"resolved"}` // без resolution
	req := httptest.NewRequest(http.MethodPatch,
		"/api/incidents/"+inc.ID, strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", inc.UpdatedAt.Format(time.RFC3339Nano))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestAddIncidentNoteAndList(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	inc := seedIncident(t, store)

	// POST заметка
	body := `{"content":"первая заметка расследователя"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/incidents/"+inc.ID+"/notes", strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleSecurityOfficer))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST note: code = %d (%s)", rec.Code, rec.Body)
	}
	var note incidentNoteDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatalf("note JSON: %v", err)
	}
	if note.Content != "первая заметка расследователя" {
		t.Errorf("content некорректный: %q", note.Content)
	}

	// GET список
	req2 := httptest.NewRequest(http.MethodGet,
		"/api/incidents/"+inc.ID+"/notes", nil)
	req2.Header.Set("Authorization", roleToken(auth.RoleAuditor))
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET notes: code = %d", rec2.Code)
	}
	var list incidentNoteListDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &list); err != nil {
		t.Fatalf("list JSON: %v", err)
	}
	if len(list.Notes) != 1 {
		t.Errorf("ожидалась 1 заметка, получено %d", len(list.Notes))
	}
}

func TestAddIncidentNoteForbiddenForAuditor(t *testing.T) {
	router, store, closeStore := fullTestRouter(t)
	defer closeStore()
	inc := seedIncident(t, store)
	body := `{"content":"попытка от auditor"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/incidents/"+inc.ID+"/notes", strings.NewReader(body))
	req.Header.Set("Authorization", roleToken(auth.RoleAuditor))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403", rec.Code)
	}
}

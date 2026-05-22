package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

type fakeDevLoginStore struct {
	userID string
	err    error
	asked  string
}

func (f *fakeDevLoginStore) UserIDForRole(_ context.Context, role string) (string, error) {
	f.asked = role
	if f.err != nil {
		return "", f.err
	}
	return f.userID, nil
}

func TestDevLoginIssuesParseableToken(t *testing.T) {
	store := &fakeDevLoginStore{userID: "u-1"}
	h := devLoginHandler(store, "test-secret")

	body, _ := json.Marshal(devLoginRequest{Role: "user"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	var resp devLoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.UserID != "u-1" || resp.Role != "user" {
		t.Errorf("неожиданный ответ: %+v", resp)
	}
	if resp.ExpiresAt == "" {
		t.Error("expires_at не должно быть пустым")
	}
	role, err := auth.ParseToken(resp.Token, "test-secret")
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if role != auth.RoleUser {
		t.Errorf("role = %s, ожидалось user", role)
	}
	if store.asked != "user" {
		t.Errorf("store asked = %q", store.asked)
	}
}

func TestDevLoginRejectsUnknownRole(t *testing.T) {
	h := devLoginHandler(&fakeDevLoginStore{}, "s")
	body, _ := json.Marshal(devLoginRequest{Role: "godmode"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDevLoginRejectsMissingDevUser(t *testing.T) {
	store := &fakeDevLoginStore{err: storage.ErrUserNotFound}
	h := devLoginHandler(store, "s")
	body, _ := json.Marshal(devLoginRequest{Role: "user"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDevLoginRejectsInvalidJSON(t *testing.T) {
	h := devLoginHandler(&fakeDevLoginStore{}, "s")
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader([]byte("{")))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestDevLoginGenericStorageError(t *testing.T) {
	store := &fakeDevLoginStore{err: errors.New("db down")}
	h := devLoginHandler(store, "s")
	body, _ := json.Marshal(devLoginRequest{Role: "user"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/dev-login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
)

const apiTestSecret = "api-test-secret"

func apiTestRouter() http.Handler {
	return NewRouter(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:      nil, // /api/policies/test и /health не обращаются к БД
		AuthSecret: apiTestSecret,
	})
}

func userToken() string {
	return "Bearer " + auth.IssueToken(auth.RoleUser, apiTestSecret)
}

func TestPolicyTestEndpointRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, httptest.NewRequest(
		http.MethodPost, "/api/policies/test", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("без токена: code = %d, ожидалось 401", rec.Code)
	}
}

func TestPolicyTestEndpointReturnsDecision(t *testing.T) {
	body := `{"model_trust":"external","risk":{"level":"medium",` +
		`"classes":["pii"],"score":0.5},"entity_types":["INN"],` +
		`"user_role":"user","context":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/policies/test",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200 (тело: %s)", rec.Code, rec.Body)
	}
	var decision struct {
		Decision string   `json:"decision"`
		Reasons  []string `json:"reasons"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decision); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	// external + ПДн → обезличивание (см. internal/policy decision-table)
	if decision.Decision != "allow_masked" {
		t.Errorf("decision = %q, ожидалось allow_masked", decision.Decision)
	}
	if len(decision.Reasons) == 0 {
		t.Error("решение без причин")
	}
}

func TestPolicyTestEndpointSecretIsDenied(t *testing.T) {
	body := `{"model_trust":"trusted_local","risk":{"level":"high",` +
		`"classes":["secret"],"score":0.9},"entity_types":["SECRET_JWT"],` +
		`"user_role":"user","context":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/policies/test",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, req)

	var decision struct {
		Decision string `json:"decision"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &decision)
	if decision.Decision != "deny" {
		t.Errorf("секрет: decision = %q, ожидалось deny", decision.Decision)
	}
}

func TestPolicyTestEndpointRejectsBadJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/policies/test",
		bytes.NewBufferString("{не json"))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("битый JSON: code = %d, ожидалось 400", rec.Code)
	}
}

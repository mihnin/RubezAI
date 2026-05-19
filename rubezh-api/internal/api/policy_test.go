package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
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

func TestPolicyTestEndpointEscalate(t *testing.T) {
	body := `{"model_trust":"on_prem","risk":{"level":"critical",` +
		`"classes":["pii"],"score":0.95},"entity_types":[],` +
		`"user_role":"user","context":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/policies/test",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, req)
	var d struct {
		Decision string `json:"decision"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Decision != "escalate" {
		t.Errorf("decision = %q, ожидалось escalate", d.Decision)
	}
}

func TestPolicyTestResponseMatchesContract(t *testing.T) {
	body := `{"model_trust":"external","risk":{"level":"medium",` +
		`"classes":["pii"],"score":0.5},"entity_types":["INN"],` +
		`"user_role":"user","context":"chat"}`
	req := httptest.NewRequest(http.MethodPost, "/api/policies/test",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	apiTestRouter().ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, ожидалось application/json", ct)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	// набор полей == PolicyDecision из policy.schema.json
	want := map[string]bool{
		"decision": true, "matched_policy_id": true,
		"matched_policy_version": true, "matched_rule": true, "reasons": true,
	}
	for k := range resp {
		if !want[k] {
			t.Errorf("лишнее поле ответа: %q", k)
		}
	}
	for k := range want {
		if _, ok := resp[k]; !ok {
			t.Errorf("в ответе нет поля %q", k)
		}
	}
	validDecisions := map[string]bool{
		"allow_raw": true, "allow_masked": true, "allow_summary_only": true,
		"deny": true, "escalate": true,
	}
	if d, _ := resp["decision"].(string); !validDecisions[d] {
		t.Errorf("decision %q вне enum контракта", d)
	}
	if reasons, _ := resp["reasons"].([]any); len(reasons) == 0 {
		t.Error("reasons пуст (контракт требует minItems=1)")
	}
}

// dbRouter строит роутер с реальным Store или пропускает тест без БД.
func dbRouter(t *testing.T) (http.Handler, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест пропущен")
	}
	store, err := storage.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	if err := store.Ping(context.Background()); err != nil {
		store.Close()
		t.Skipf("БД недоступна: %v", err)
	}
	router := NewRouter(Deps{
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Store:      store,
		AuthSecret: apiTestSecret,
	})
	return router, store.Close
}

func TestCreatePolicyEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()

	name := "api-policy-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","description":"через API"}`
	req := httptest.NewRequest(http.MethodPost, "/api/policies",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, ожидалось 201 (тело: %s)", rec.Code, rec.Body)
	}
	var created policyDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	if created.ID == "" || created.Name != name || created.CurrentVersion != 1 {
		t.Errorf("некорректный ответ: %+v", created)
	}

	// дубликат имени → 409, а не 500
	dup := httptest.NewRequest(http.MethodPost, "/api/policies",
		bytes.NewBufferString(body))
	dup.Header.Set("Authorization", userToken())
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dup)
	if dupRec.Code != http.StatusConflict {
		t.Errorf("дубликат: code = %d, ожидалось 409", dupRec.Code)
	}
}

func TestCreatePolicyEndpointRequiresAuth(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/policies",
		bytes.NewBufferString(`{"name":"x"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestCreatePolicyEndpointRejectsEmptyName(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPost, "/api/policies",
		bytes.NewBufferString(`{"name":""}`))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestListPoliciesEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/policies", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200", rec.Code)
	}
	var policies []policyDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &policies); err != nil {
		t.Fatalf("ответ не JSON-массив: %v", err)
	}
}

func TestListPoliciesEndpointRequiresAuth(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/policies", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
)

func TestListModelsRequiresAuth(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestCreateModelRequiresAuth(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(`{"name":"x"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestCreateModelEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()

	name := "api-model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"trusted_local","adapter":"mock"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, ожидалось 201 (тело: %s)", rec.Code, rec.Body)
	}
	var created modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	if created.ID == "" || created.Name != name || !created.IsEnabled {
		t.Errorf("некорректный ответ: %+v", created)
	}

	dup := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	dup.Header.Set("Authorization", userToken())
	dupRec := httptest.NewRecorder()
	router.ServeHTTP(dupRec, dup)
	if dupRec.Code != http.StatusConflict {
		t.Errorf("дубликат: code = %d, ожидалось 409", dupRec.Code)
	}
}

func TestCreateModelRejectsInvalidTrustLevel(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	body := `{"name":"x","trust_level":"hacker_cloud","adapter":"mock"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateModelOpenAIRequiresEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	// adapter openai_compatible без endpoint недопустим
	body := `{"name":"x","trust_level":"external","adapter":"openai_compatible"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestListModelsEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200", rec.Code)
	}
	var providers []modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &providers); err != nil {
		t.Fatalf("ответ не JSON-массив: %v", err)
	}
}

func TestCreateModelRejectsBadJSON(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString("{битый"))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateModelResponseFields(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "fields-model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"russian_cloud","adapter":"mock"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d", rec.Code)
	}
	var created modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.TrustLevel != "russian_cloud" || created.Adapter != "mock" {
		t.Errorf("поля trust/adapter некорректны: %+v", created)
	}
	if created.Endpoint != "" {
		t.Errorf("Endpoint = %q, ожидалось пусто", created.Endpoint)
	}
	if !strings.Contains(rec.Body.String(), `"max_tokens":null`) {
		t.Errorf("max_tokens должен сериализоваться как null: %s", rec.Body)
	}
}

func TestCreateModelPersistsNullableFields(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "nullable-model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"external",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local",` +
		`"max_tokens":2048,"rate_limit_per_min":60}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d (тело %s)", rec.Code, rec.Body)
	}
	var created modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.MaxTokens == nil || *created.MaxTokens != 2048 {
		t.Error("max_tokens не сохранён")
	}
	if created.RateLimitPerMin == nil || *created.RateLimitPerMin != 60 {
		t.Error("rate_limit_per_min не сохранён")
	}
	if created.Endpoint != "http://llm.local" {
		t.Errorf("Endpoint = %q", created.Endpoint)
	}
}

func TestCreateModelRejectsInvalidAdapter(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	body := `{"name":"x","trust_level":"external","adapter":"langchain"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateModelRejectsMissingName(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	for _, body := range []string{
		`{"trust_level":"external","adapter":"mock"}`,
		`{"name":"","trust_level":"external","adapter":"mock"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/models",
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", userToken())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, ожидалось 400", body, rec.Code)
		}
	}
}

func TestCreateModelRejectsNonPositiveLimits(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	for _, body := range []string{
		`{"name":"x","trust_level":"external","adapter":"mock","max_tokens":0}`,
		`{"name":"x","trust_level":"external","adapter":"mock","max_tokens":-5}`,
		`{"name":"x","trust_level":"external","adapter":"mock","rate_limit_per_min":0}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/models",
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", userToken())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, ожидалось 400", body, rec.Code)
		}
	}
}

func TestCreateModelAcceptsAllTrustLevels(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	for _, trust := range []string{
		"external", "russian_cloud", "on_prem", "trusted_local",
	} {
		name := "trust-" + trust + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		body := `{"name":"` + name + `","trust_level":"` + trust + `","adapter":"mock"}`
		req := httptest.NewRequest(http.MethodPost, "/api/models",
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", userToken())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Errorf("trust %q: code = %d, ожидалось 201", trust, rec.Code)
		}
	}
}

func TestModelsEndpointRejectsWrongMethod(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodDelete, "/api/models", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/models: code = %d, ожидалось 405", rec.Code)
	}
}

func TestCreateModelRejectsEmptyBody(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPost, "/api/models", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("пустое тело: code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateModelRejectsForeignToken(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(`{"name":"x","trust_level":"external","adapter":"mock"}`))
	req.Header.Set("Authorization",
		"Bearer "+auth.IssueToken(auth.RoleUser, "wrong-secret"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("чужой секрет: code = %d, ожидалось 401", rec.Code)
	}
}

func TestModelsResponseDoesNotLeakApiKey(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "leakcheck-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"external",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local"}`
	post := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	post.Header.Set("Authorization", userToken())
	router.ServeHTTP(httptest.NewRecorder(), post)

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	raw := rec.Body.String()
	// has_api_key (bool флаг) — публичен в Итерации 9.5;
	// сам ключ (как поле "api_key": или его ciphertext) не должен быть.
	for _, leak := range []string{"Bearer", `"api_key"`, "apiKey",
		"api_key_encrypted"} {
		if strings.Contains(raw, leak) {
			t.Errorf("ответ /api/models содержит подозрительную подстроку %q", leak)
		}
	}
	if !strings.Contains(raw, `"has_api_key":`) {
		t.Errorf("ответ /api/models должен содержать has_api_key: %s", raw)
	}
}

func TestCreateModelRejectsMalformedEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	for _, endpoint := range []string{
		"не-url", "ftp://llm.local", "llm.local", "http://", "https://",
	} {
		body := `{"name":"x","trust_level":"external",` +
			`"adapter":"openai_compatible","endpoint":"` + endpoint + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/models",
			bytes.NewBufferString(body))
		req.Header.Set("Authorization", userToken())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("endpoint %q: code = %d, ожидалось 400", endpoint, rec.Code)
		}
	}
}

func TestCreateModelRejectsUnknownField(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	// неизвестное поле в теле должно отклоняться (DisallowUnknownFields)
	body := `{"name":"x","trust_level":"external","adapter":"mock","trust_lvl":"oops"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("неизвестное поле: code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateModelRejectsTrailingData(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	// хвостовые данные после JSON-значения должны отклоняться
	body := `{"name":"x","trust_level":"external","adapter":"mock"}{"name":"y"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("хвостовые данные: code = %d, ожидалось 400", rec.Code)
	}
}

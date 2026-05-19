package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
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

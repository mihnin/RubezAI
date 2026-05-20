package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// TestCreateModelForbiddenForUser — MAJOR-1 ревью Итерации 9.5:
// security-критичный POST /api/models требует admin/developer.
func TestCreateModelForbiddenForUser(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	body := `{"name":"u-no","trust_level":"trusted_local",` +
		`"adapter":"openai_compatible","endpoint":"http://x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403 (user не может создать модель)",
			rec.Code)
	}
}

// TestUpdateAPIKeyForbiddenForUser — RBAC на ротацию ключа.
func TestUpdateAPIKeyForbiddenForUser(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPost,
		"/api/models/00000000-0000-0000-0000-000000000001/api-key",
		bytes.NewBufferString(`{"api_key":"x"}`))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403", rec.Code)
	}
}

// TestCreateModelWithAPIKey — POST /api/models с api_key:
// шифруется и сохраняется; в ответе has_api_key=true,
// сам ключ не возвращается.
func TestCreateModelWithAPIKey(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "withkey-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"trusted_local",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local",` +
		`"api_key":"sk-test-secret-12345"}`
	post := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	post.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d (тело %s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("response не JSON: %v", err)
	}
	if !dto.HasAPIKey {
		t.Error("has_api_key должен быть true для созданного провайдера с ключом")
	}
	if strings.Contains(rec.Body.String(), "sk-test-secret-12345") {
		t.Errorf("plaintext api_key утёк в ответ: %s", rec.Body.String())
	}
}

// TestCreateModelWithoutAPIKey — без api_key has_api_key=false.
func TestCreateModelWithoutAPIKey(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "nokey-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"trusted_local",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local"}`
	post := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	post.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d (тело %s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.HasAPIKey {
		t.Error("has_api_key должен быть false без api_key в запросе")
	}
}

// TestUpdateModelAPIKey — POST /api/models/:id/api-key меняет ключ.
func TestUpdateModelAPIKey(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	// Создаём провайдер без ключа.
	name := "updkey-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"trusted_local",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local"}`
	post := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	post.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, post)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code = %d (%s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.HasAPIKey {
		t.Error("initially has_api_key должен быть false")
	}

	// Обновляем ключ.
	upd := httptest.NewRequest(http.MethodPost,
		"/api/models/"+dto.ID+"/api-key",
		bytes.NewBufferString(`{"api_key":"new-secret-99"}`))
	upd.Header.Set("Authorization", adminToken())
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, upd)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("update code = %d (%s)", rec2.Code, rec2.Body)
	}

	// GET /api/models — has_api_key теперь true.
	getReq := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	getReq.Header.Set("Authorization", adminToken())
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, getReq)
	var list []modelProviderDTO
	_ = json.Unmarshal(rec3.Body.Bytes(), &list)
	var found bool
	for _, p := range list {
		if p.ID == dto.ID {
			found = true
			if !p.HasAPIKey {
				t.Error("после update has_api_key должен быть true")
			}
		}
	}
	if !found {
		t.Errorf("провайдер %s не найден в списке", dto.ID)
	}

	// Plaintext не утекает.
	if strings.Contains(rec3.Body.String(), "new-secret-99") {
		t.Errorf("plaintext в GET /api/models: %s", rec3.Body.String())
	}
}

// TestUpdateModelAPIKeyEmptyClears — пустая строка очищает ключ.
func TestUpdateModelAPIKeyEmptyClears(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	name := "clrkey-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"trusted_local",` +
		`"adapter":"openai_compatible","endpoint":"http://llm.local",` +
		`"api_key":"initial"}`
	post := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	post.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, post)
	var dto modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)

	// Очищаем ключ.
	upd := httptest.NewRequest(http.MethodPost,
		"/api/models/"+dto.ID+"/api-key",
		bytes.NewBufferString(`{"api_key":""}`))
	upd.Header.Set("Authorization", adminToken())
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, upd)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("clear code = %d (%s)", rec2.Code, rec2.Body)
	}

	// Проверяем storage напрямую.
	store := dbStoreFromRouter(t)
	defer store.Close()
	p, err := store.GetModelProvider(context.Background(), dto.ID)
	if err != nil {
		t.Fatalf("GetModelProvider: %v", err)
	}
	if p.HasAPIKey() {
		t.Error("после очистки HasAPIKey должен быть false")
	}
}

// dbStoreFromRouter — отдельный Storage для прямой проверки БД в тесте.
func dbStoreFromRouter(t *testing.T) *storage.Storage {
	t.Helper()
	s, _ := dbStoreClose(t)
	return s
}

// dbStoreClose — отдельный Storage с auto-close через t.Cleanup.
func dbStoreClose(t *testing.T) (*storage.Storage, func()) {
	return dbStore(t)
}

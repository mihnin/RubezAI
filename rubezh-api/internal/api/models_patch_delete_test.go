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

// createTestProvider создаёт провайдера через API и возвращает его DTO.
func createTestProvider(t *testing.T, router http.Handler) modelProviderDTO {
	t.Helper()
	name := "pd-model-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body := `{"name":"` + name + `","trust_level":"external","adapter":"mock"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("создание провайдера: code = %d (тело: %s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	return dto
}

func TestPatchModelTogglesEnabled(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	for _, want := range []bool{false, true} {
		body, _ := json.Marshal(patchModelRequest{IsEnabled: &want})
		req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
			bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken())
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH is_enabled=%v: code = %d (тело: %s)", want, rec.Code, rec.Body)
		}
		var dto modelProviderDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
			t.Fatalf("ответ не JSON: %v", err)
		}
		if dto.IsEnabled != want {
			t.Errorf("is_enabled = %v, ожидалось %v", dto.IsEnabled, want)
		}
	}
}

func TestPatchModelForbiddenForUser(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	body := `{"is_enabled":false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403", rec.Code)
	}
}

func TestPatchModelNotFound(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodPatch,
		"/api/models/00000000-0000-0000-0000-000000000000",
		bytes.NewBufferString(`{"is_enabled":false}`))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, ожидалось 404", rec.Code)
	}
}

// TestPatchModelUpdatesDefaultModel — миграция 000019: PATCH должен
// уметь менять default_model. Полезно для админ-UI и CLI, чтобы
// переключать модели без правки кода.
func TestPatchModelUpdatesDefaultModel(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	model := "claude-opus-4-7"
	body, _ := json.Marshal(patchModelRequest{DefaultModel: &model})
	req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewReader(body))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH default_model: code = %d (тело: %s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	if dto.DefaultModel != model {
		t.Errorf("default_model = %q, ожидалось %q", dto.DefaultModel, model)
	}
}

// TestPatchModelClearsDefaultModel — пустая строка допустима: адаптер
// откатится к встроенному fallback. Используется когда нужно «сбросить»
// дефолт, чтобы запрос шёл по адаптерной логике.
func TestPatchModelClearsDefaultModel(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	// Сначала ставим, потом чистим.
	first := "preset-model"
	body, _ := json.Marshal(patchModelRequest{DefaultModel: &first})
	req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewReader(body))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("первый PATCH: code = %d (тело: %s)", rec.Code, rec.Body)
	}

	empty := ""
	body, _ = json.Marshal(patchModelRequest{DefaultModel: &empty})
	req = httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewReader(body))
	req.Header.Set("Authorization", adminToken())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("очистка default_model: code = %d (тело: %s)", rec.Code, rec.Body)
	}
	var dto modelProviderDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &dto)
	if dto.DefaultModel != "" {
		t.Errorf("default_model = %q, ожидалось пусто", dto.DefaultModel)
	}
}

// TestPatchModelDefaultModelExposedInDTO — список провайдеров возвращает
// default_model в ответе (контракт с UI).
func TestPatchModelDefaultModelExposedInDTO(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	model := "gpt-5.3-codex"
	body, _ := json.Marshal(patchModelRequest{DefaultModel: &model})
	req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewReader(body))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: code = %d (тело: %s)", rec.Code, rec.Body)
	}

	// GET /api/models — провайдер должен показать default_model.
	req = httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", adminToken())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: code = %d", rec.Code)
	}
	var list []modelProviderDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("список не JSON: %v", err)
	}
	found := false
	for _, p := range list {
		if p.ID == provider.ID {
			found = true
			if p.DefaultModel != model {
				t.Errorf("default_model в GET = %q, ожидалось %q",
					p.DefaultModel, model)
			}
		}
	}
	if !found {
		t.Errorf("созданный провайдер %s не найден в GET /api/models", provider.ID)
	}
}

func TestPatchModelEmptyBodyRejected(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)
	req := httptest.NewRequest(http.MethodPatch, "/api/models/"+provider.ID,
		bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestDeleteModelUnreferenced(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)

	req := httptest.NewRequest(http.MethodDelete, "/api/models/"+provider.ID, nil)
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, ожидалось 204 (тело: %s)", rec.Code, rec.Body)
	}
	// повторное удаление → 404
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/models/"+provider.ID, nil)
	req2.Header.Set("Authorization", adminToken())
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("повторное удаление: code = %d, ожидалось 404", rec2.Code)
	}
}

func TestDeleteModelForbiddenForUser(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	provider := createTestProvider(t, router)
	req := httptest.NewRequest(http.MethodDelete, "/api/models/"+provider.ID, nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("code = %d, ожидалось 403", rec.Code)
	}
}

func TestDeleteModelNotFound(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/models/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("Authorization", adminToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, ожидалось 404", rec.Code)
	}
}

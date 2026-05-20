package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/chat"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// dbStore открывает тестовый Storage или пропускает тест без БД.
func dbStore(t *testing.T) (*storage.Storage, func()) {
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
	return store, store.Close
}

// registerProvider создаёт mock-провайдера в БД и в llm.Router.
func registerProvider(t *testing.T, store *storage.Storage, router *llm.Router) string {
	t.Helper()
	name := "chat-prov-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := store.CreateModelProvider(context.Background(), storage.ModelProvider{
		Name: name, TrustLevel: "trusted_local", Adapter: "mock",
	}); err != nil {
		t.Fatalf("CreateModelProvider: %v", err)
	}
	router.Register(llm.NewMockProvider(name))
	return name
}

// fakeChatOrchestrator — заглушка оркестратора для тестов HTTP-слоя.
// Реализует Prepare/Stream — раздельная подготовка и стрим (MAJOR-2 плана).
type fakeChatOrchestrator struct {
	gotReq      chat.Request
	prepareErr  error
	streamErr   error
	streamFails bool // если true — Stream завершится sink.Fail вместо Done
}

func (f *fakeChatOrchestrator) Prepare(
	_ context.Context, req chat.Request,
) (chat.Prepared, error) {
	f.gotReq = req
	return chat.Prepared{}, f.prepareErr
}

func (f *fakeChatOrchestrator) Stream(
	_ context.Context, req chat.Request,
	_ chat.Prepared, sink chat.EventSink,
) error {
	_ = sink.Meta(chat.MetaEvent{
		Decision: "allow_raw", Provider: req.Provider,
		Reasons: []string{}, RequestID: req.RequestID,
	})
	if f.streamFails {
		return sink.Fail("тестовый сбой", req.RequestID)
	}
	_ = sink.Delta("тестовый ответ")
	if err := sink.Done(req.RequestID); err != nil {
		return err
	}
	return f.streamErr
}

// chatTestHandler собирает обработчик /api/chat с auth для прямого вызова.
func chatTestHandler(
	orch chatOrchestrator, store *storage.Storage, router *llm.Router,
) http.Handler {
	return auth.Middleware(apiTestSecret)(
		chatHandler(orch, store, router, discardLogger()))
}

func TestChatHandlerStreamsEvents(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)
	orch := &fakeChatOrchestrator{}

	body := `{"message":"привет","provider":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(orch, store, router).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200 (тело %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}
	out := rec.Body.String()
	for _, want := range []string{"event: meta", "event: delta", "event: done"} {
		if !strings.Contains(out, want) {
			t.Errorf("SSE-поток не содержит %q: %s", want, out)
		}
	}
	if orch.gotReq.Provider != name || orch.gotReq.ModelTrust != "trusted_local" {
		t.Errorf("chat.Request некорректен: %+v", orch.gotReq)
	}
	if orch.gotReq.UserID == "" || orch.gotReq.RequestID == "" {
		t.Error("UserID/RequestID не заполнены")
	}
	// Контракт chat.schema.json#SseMeta/#SseDone требует request_id во всех
	// терминальных payload'ах — это критический коррелятор для расследования.
	wantID := `"request_id":"` + orch.gotReq.RequestID + `"`
	if !strings.Contains(out, wantID) {
		t.Errorf("SSE-поток не содержит request_id (%s) ни в meta, ни в done: %s",
			orch.gotReq.RequestID, out)
	}
}

// TestChatHandlerErrorEventCarriesRequestID — закрывает M2 ревью этапа A:
// SSE event:error должен нести request_id (chat.schema.json#SseError).
func TestChatHandlerErrorEventCarriesRequestID(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)
	orch := &fakeChatOrchestrator{streamFails: true}

	body := `{"message":"привет","provider":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(orch, store, router).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200 (SSE открыт до Fail)", rec.Code)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: error") {
		t.Fatalf("SSE-поток не содержит event: error: %s", out)
	}
	wantID := `"request_id":"` + orch.gotReq.RequestID + `"`
	if !strings.Contains(out, wantID) {
		t.Errorf("event:error не содержит request_id (%s): %s",
			orch.gotReq.RequestID, out)
	}
	if !strings.Contains(out, `"message":"тестовый сбой"`) {
		t.Errorf("event:error не содержит message: %s", out)
	}
}

func TestChatHandlerRequiresAuth(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	rec := httptest.NewRecorder()
	chatTestHandler(&fakeChatOrchestrator{}, store, router).ServeHTTP(
		rec, httptest.NewRequest(http.MethodPost, "/api/chat",
			bytes.NewBufferString(`{"message":"x","provider":"p"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestChatHandlerRejectsEmptyMessage(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)

	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(`{"message":"","provider":"`+name+`"}`))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(&fakeChatOrchestrator{}, store, router).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("пустое сообщение: code = %d, ожидалось 400", rec.Code)
	}
}

func TestChatHandlerRejectsUnknownProvider(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()

	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(`{"message":"привет","provider":"нет-провайдера"}`))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(&fakeChatOrchestrator{}, store, router).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("неизвестный провайдер: code = %d, ожидалось 400", rec.Code)
	}
}

func TestChatHandlerRejectsUnknownField(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)

	body := `{"message":"привет","provider":"` + name + `","inject":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(&fakeChatOrchestrator{}, store, router).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("неизвестное поле: code = %d, ожидалось 400", rec.Code)
	}
}

func TestChatHandlerRejectsOversizedMessage(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)

	huge, _ := json.Marshal(strings.Repeat("я", 16385))
	body := `{"message":` + string(huge) + `,"provider":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(&fakeChatOrchestrator{}, store, router).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("слишком длинное сообщение: code = %d, ожидалось 400", rec.Code)
	}
}

func TestCreateChatSessionEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()

	req := httptest.NewRequest(http.MethodPost, "/api/chat/sessions",
		bytes.NewBufferString(`{"title":"Моя сессия"}`))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, ожидалось 201 (тело %s)", rec.Code, rec.Body)
	}
	var s chatSessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	if s.ID == "" || s.UserID == "" {
		t.Errorf("сессия создана некорректно: %+v", s)
	}
	if s.Title == nil || *s.Title != "Моя сессия" {
		t.Errorf("title = %v", s.Title)
	}
}

func TestListChatSessionsEndpoint(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()

	req := httptest.NewRequest(http.MethodGet, "/api/chat/sessions", nil)
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200", rec.Code)
	}
	var sessions []chatSessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("ответ не JSON-массив: %v", err)
	}
}

func TestChatSessionsRequireAuth(t *testing.T) {
	router, closeStore := dbRouter(t)
	defer closeStore()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(
		http.MethodGet, "/api/chat/sessions", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, ожидалось 401", rec.Code)
	}
}

func TestChatHandlerPrepareError(t *testing.T) {
	// Сбой подготовки (sanitizer/Tx1/спан) — HTTP 502, без SSE-заголовков
	// и без event:* в теле. Закрывает MAJOR-2 ревью архитектора.
	store, closeStore := dbStore(t)
	defer closeStore()
	router := llm.NewRouter()
	name := registerProvider(t, store, router)
	orch := &fakeChatOrchestrator{prepareErr: errors.New("sanitizer недоступен")}

	body := `{"message":"привет","provider":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	chatTestHandler(orch, store, router).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("сбой подготовки: code = %d, ожидалось 502", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Error("SSE-заголовки не должны выставляться при сбое подготовки")
	}
	if strings.Contains(rec.Body.String(), "event:") {
		t.Errorf("при сбое подготовки не должны отправляться SSE-события: %s",
			rec.Body.String())
	}
}

// TestChatEndpointFullFlow — сквозной поток с реальным оркестратором и
// живым sanitizer (пропускается без TEST_SANITIZER_URL).
func TestChatEndpointFullFlow(t *testing.T) {
	sanURL := os.Getenv("TEST_SANITIZER_URL")
	if sanURL == "" {
		t.Skip("TEST_SANITIZER_URL не задан — сквозной тест пропущен")
	}
	store, closeStore := dbStore(t)
	defer closeStore()
	llmRouter := llm.NewRouter()
	name := registerProvider(t, store, llmRouter)

	handler, _ := NewRouter(Deps{
		Logger:       discardLogger(),
		Store:        store,
		AuthSecret:   apiTestSecret,
		Router:       llmRouter,
		SanitizerURL: sanURL,
	})

	body := `{"message":"Какая погода завтра в Москве","provider":"` + name + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat",
		bytes.NewBufferString(body))
	req.Header.Set("Authorization", userToken())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, ожидалось 200 (тело %s)", rec.Code, rec.Body)
	}
	out := rec.Body.String()
	for _, want := range []string{"event: meta", "event: delta", "event: done"} {
		if !strings.Contains(out, want) {
			t.Errorf("SSE-поток не содержит %q: %s", want, out)
		}
	}
}

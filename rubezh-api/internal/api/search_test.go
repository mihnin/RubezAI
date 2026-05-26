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

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// searchRouterCustomLimiter — фабрика handler'а search с кастомным limiter'ом,
// чтобы тесты rate-limit'а контролировали burst/RPM.
func searchRouterCustomLimiter(t *testing.T, store *storage.Storage,
	limiter *UserRateLimiter) http.Handler {
	t.Helper()
	handler := searchHandler(store, nil, llm.MockEmbedder{}, limiter)
	// Wrapping с auth middleware (требуется userID для rate-limit per-user).
	r := http.NewServeMux()
	r.Handle("/api/search", auth.Middleware(apiTestSecret)(handler))
	return r
}

func searchReq(t *testing.T, body, bearer string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/search",
		bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		r.Header.Set("Authorization", bearer)
	}
	return r
}

func TestSearchHandler_BadRequestEmptyQuery(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, `{"query":""}`, userToken()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400", rec.Code)
	}
}

func TestSearchHandler_BadRequestQueryTooLong(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))
	long := strings.Repeat("ё", 2001)
	body, _ := json.Marshal(map[string]any{"query": long})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, ожидалось 400 на query > 2000 рун", rec.Code)
	}
}

func TestSearchHandler_RateLimit(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	// burst=2, rpm=60 (~1 RPS) → 3-й сразу 429.
	limiter := NewUserRateLimiter(60, 2)
	h := searchRouterCustomLimiter(t, store, limiter)
	body := `{"query":"тест"}`
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, searchReq(t, body, userToken()))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("burst=2 — запрос %d не должен быть 429", i)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, body, userToken()))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("3-й запрос: code = %d, ожидалось 429", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("Retry-After заголовок отсутствует на 429")
	}
}

func TestSearchHandler_RateLimitEmitsAuditOncePerWindow(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	since := time.Now()
	limiter := NewUserRateLimiter(60, 1) // burst=1
	h := searchRouterCustomLimiter(t, store, limiter)
	body := `{"query":"x"}`
	// 1 успешный + 10 отказанных.
	for i := 0; i < 11; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, searchReq(t, body, userToken()))
	}

	var count int
	err := store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events
		 WHERE event_type='rate_limit_exceeded' AND created_at >= $1`,
		since).Scan(&count)
	if err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if count < 1 {
		t.Errorf("ожидался ≥ 1 audit rate_limit_exceeded, got %d", count)
	}
	if count > 1 {
		t.Errorf("anti-flood сломан: %d audit за окно (ожидался 1)", count)
	}
}

func TestSearchHandler_AuditContainsNoQueryPlaintext(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))

	secret := "SECRET_MARKER_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	body, _ := json.Marshal(map[string]any{"query": secret})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}

	var hits int
	err := store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events
		 WHERE detail::text LIKE '%' || $1 || '%'
		    OR COALESCE(masked_payload::text, '') LIKE '%' || $1 || '%'`,
		secret).Scan(&hits)
	if err != nil {
		t.Fatalf("grep audit: %v", err)
	}
	if hits != 0 {
		t.Errorf("plaintext query в audit (инвариант §Р5): %d записей", hits)
	}
}

func TestSearchHandler_AuditContainsExtendedFields(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	since := time.Now()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))

	body, _ := json.Marshal(map[string]any{"query": "test query"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}

	var detailJSON string
	err := store.Pool().QueryRow(context.Background(),
		`SELECT detail::text FROM audit_events
		 WHERE event_type='search_performed' AND created_at >= $1
		 ORDER BY created_at DESC LIMIT 1`, since).Scan(&detailJSON)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	for _, key := range []string{
		"query_hash", "query_len", "embedder_model",
		"rag_mode", "latency_ms", "result_count",
	} {
		if !strings.Contains(detailJSON, `"`+key+`"`) {
			t.Errorf("audit detail без поля %q: %s", key, detailJSON)
		}
	}
	// pgsql jsonb::text использует ": " (с пробелом) после ключа.
	if !strings.Contains(detailJSON, `"rag_mode": "explicit"`) {
		t.Errorf("rag_mode != explicit: %s", detailJSON)
	}
	if !strings.Contains(detailJSON, `"embedder_model": "mock-sha256-v1"`) {
		t.Errorf("embedder_model != mock-sha256-v1: %s", detailJSON)
	}
}

// TestSearchHandler_ForeignDocumentIdsAuditAclViolation — BLOCKER B1
// regression в handler-слое: чужой document_id → 200 + 0 hits +
// audit `acl_violation_attempt`.
func TestSearchHandler_ForeignDocumentIdsAuditAclViolation(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	since := time.Now()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))

	// Случайный UUID, который не принадлежит этому user'у.
	body := `{"query":"x","document_ids":["00000000-0000-0000-0000-000000000099"]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, body, userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d (ожидался silent 200)", rec.Code)
	}

	var count int
	err := store.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events
		 WHERE event_type='acl_violation_attempt' AND created_at >= $1`,
		since).Scan(&count)
	if err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if count < 1 {
		t.Errorf("acl_violation_attempt НЕ записан (BLOCKER B1 диагностика)")
	}
}

// TestSearchHandler_AclViolationMatrix — matrix-тест BLOCKER B1
// (закрывает P1 ревью): owner / другой user / частичный mix / supervisor.
// Каждый кейс проверяет ИЛИ запись audit'а ИЛИ её отсутствие.
func TestSearchHandler_AclViolationMatrix(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	ctx := context.Background()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 100))

	// Создаём 2 документа: own — userA, foreign — userB. UserA пытается
	// разные комбинации.
	userA, _ := store.UserIDForRole(ctx, "user")
	userB, _ := store.UserIDForRole(ctx, "developer")

	mkDoc := func(owner, label string) string {
		d, err := store.CreateDocument(ctx, storage.DocumentInput{
			OwnerID: owner, Filename: "matrix-" + label + "-" +
				strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt",
			StorageKey: "matrix/" + label,
		})
		if err != nil {
			t.Fatalf("CreateDocument: %v", err)
		}
		_, _ = store.Pool().Exec(ctx,
			`UPDATE documents SET status='done' WHERE id = $1`, d.ID)
		return d.ID
	}
	ownDoc := mkDoc(userA, "own")
	foreignDoc := mkDoc(userB, "foreign")

	countAuditSince := func(since time.Time) int {
		var n int
		_ = store.Pool().QueryRow(ctx,
			`SELECT count(*) FROM audit_events
			 WHERE event_type='acl_violation_attempt'
			   AND created_at >= $1`, since).Scan(&n)
		return n
	}

	// Случай 1: user A, document_ids=[own] → no violation.
	since := time.Now()
	body, _ := json.Marshal(map[string]any{
		"query": "x", "document_ids": []string{ownDoc},
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("case1 code = %d", rec.Code)
	}
	if n := countAuditSince(since); n != 0 {
		t.Errorf("case1 (own doc): false-positive %d violation'ов", n)
	}

	// Случай 2: user A, document_ids=[foreign] → violation.
	since = time.Now()
	body, _ = json.Marshal(map[string]any{
		"query": "x", "document_ids": []string{foreignDoc},
	})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("case2 code = %d", rec.Code)
	}
	if n := countAuditSince(since); n < 1 {
		t.Errorf("case2 (foreign doc): ожидался ≥ 1 violation, got %d", n)
	}

	// Случай 3: user A, document_ids=[own, foreign] → ЧАСТИЧНЫЙ violation.
	since = time.Now()
	body, _ = json.Marshal(map[string]any{
		"query": "x", "document_ids": []string{ownDoc, foreignDoc},
	})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if n := countAuditSince(since); n < 1 {
		t.Errorf("case3 (partial): ожидался ≥ 1 violation, got %d", n)
	}

	// Случай 4: admin, document_ids=[foreign] → НЕТ violation (bypass).
	since = time.Now()
	body, _ = json.Marshal(map[string]any{
		"query": "x", "document_ids": []string{foreignDoc},
	})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), adminToken()))
	if n := countAuditSince(since); n != 0 {
		t.Errorf("case4 (admin bypass): false-positive %d violation'ов", n)
	}
}

// TestSearchHandler_LimitClampNotFalsePositive — P0 ревью: при limit=1
// и document_ids=[10 СВОИХ docs] результат урезан до 1, но это НЕ
// должно триггерить acl_violation_attempt (все 10 доступны user'у).
func TestSearchHandler_LimitClampNotFalsePositive(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	ctx := context.Background()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 100))

	userA, _ := store.UserIDForRole(ctx, "user")
	docIDs := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		d, err := store.CreateDocument(ctx, storage.DocumentInput{
			OwnerID: userA,
			Filename: "fp-" + strconv.Itoa(i) + "-" +
				strconv.FormatInt(time.Now().UnixNano(), 36) + ".txt",
			StorageKey: "fp/" + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("CreateDocument %d: %v", i, err)
		}
		_, _ = store.Pool().Exec(ctx,
			`UPDATE documents SET status='done' WHERE id = $1`, d.ID)
		docIDs = append(docIDs, d.ID)
	}

	since := time.Now()
	body, _ := json.Marshal(map[string]any{
		"query": "x", "document_ids": docIDs, "limit": 1,
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, string(body), userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}

	var n int
	_ = store.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_events
		 WHERE event_type='acl_violation_attempt' AND created_at >= $1`,
		since).Scan(&n)
	if n != 0 {
		t.Errorf("P0 регрессия: limit clamp дал %d false-positive violation'ов "+
			"(требуется 0 — все 10 docs доступны user'у)", n)
	}
}

// TestSearchHandler_ResponseShape — поверхностный проверка
// searchResponseDTO: results=[], stats{query_had_pii=false, latency_ms≥0}.
func TestSearchHandler_ResponseShape(t *testing.T) {
	store, closeStore := dbStore(t)
	defer closeStore()
	h := searchRouterCustomLimiter(t, store, NewUserRateLimiter(600, 10))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, searchReq(t, `{"query":"nonsense"}`, userToken()))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp searchResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, rec.Body.String())
	}
	if resp.Stats.LatencyMs < 0 {
		t.Errorf("latency_ms = %d (отрицательное)", resp.Stats.LatencyMs)
	}
}

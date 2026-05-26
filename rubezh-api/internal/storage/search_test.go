package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rubezh-ai/rubezh-api/internal/testdb"
)

// truncateRunes — это unit-тест, не требует БД.
func TestTruncateRunesShortStringUnchanged(t *testing.T) {
	if got := truncateRunes("abc", 10); got != "abc" {
		t.Errorf("got %q, want %q", got, "abc")
	}
}

func TestTruncateRunesAsciiBoundary(t *testing.T) {
	if got := truncateRunes("abcdefg", 3); got != "abc" {
		t.Errorf("got %q, want %q", got, "abc")
	}
}

// TestTruncateRunesCyrillicBoundary — UTF-8: кириллица 2 байта/руна,
// truncate по rune, не по байту, чтобы не разрезать символ.
func TestTruncateRunesCyrillicBoundary(t *testing.T) {
	s := "Привет, мир!"
	got := truncateRunes(s, 7)
	if got != "Привет," {
		t.Errorf("got %q, want %q", got, "Привет,")
	}
}

func TestTruncateRunesEmojiBoundary(t *testing.T) {
	// эмодзи может быть 4 байта; truncate по руне сохраняет целостность
	s := "ab😀cd"
	got := truncateRunes(s, 3)
	if got != "ab😀" {
		t.Errorf("got %q, want %q", got, "ab😀")
	}
}

func TestTruncateRunesZeroLimit(t *testing.T) {
	if got := truncateRunes("anything", 0); got != "" {
		t.Errorf("zero limit → пустая строка, got %q", got)
	}
}

// --- Интеграционные тесты ниже требуют TEST_DATABASE_URL ---

// searchTestCtx — общий setup для интеграционных тестов SearchChunks.
type searchTestCtx struct {
	store      *Storage
	userA      string
	userB      string
	adminID    string
	queryVec   []float32 // per-test уникальный вектор (защита от поллюции БД)
	closeVec   []float32 // == queryVec → distance 0 → top-1
	farVec     []float32 // ортогонален → distance 1
	docA       string    // owner=userA, нет acl
	docB       string    // owner=userB, нет acl
	docShared  string    // owner=userA, acl=[{user_id:userB}]
	docRole    string    // owner=userA, acl=[{role:developer}]
	docDeleted string    // owner=userA, status=deleted
	docPending string    // owner=userA, status=pending (нет embeddings)
}

// setupSearchTest создаёт фикстуру: 3 пользователя, 6 документов
// с чанками и embeddings (по 2 чанка на done-документ).
//
//nolint:gocyclo
func setupSearchTest(t *testing.T, store *Storage) *searchTestCtx {
	t.Helper()
	ctx := context.Background()
	prefix := testdb.TestNameUnique(t, "search")

	userA, err := store.UserIDForRole(ctx, "user")
	if err != nil {
		t.Fatalf("UserIDForRole(user): %v", err)
	}
	userB, err := store.UserIDForRole(ctx, "developer")
	if err != nil {
		t.Fatalf("UserIDForRole(developer): %v", err)
	}
	adminID, err := store.UserIDForRole(ctx, "admin")
	if err != nil {
		t.Fatalf("UserIDForRole(admin): %v", err)
	}

	mkDoc := func(name, ownerID, acl, status string) string {
		t.Helper()
		var aclJSON json.RawMessage
		if acl != "" {
			aclJSON = json.RawMessage(acl)
		}
		d, err := store.CreateDocument(ctx, DocumentInput{
			OwnerID:    ownerID,
			Filename:   prefix + "_" + name + ".txt",
			StorageKey: prefix + "/" + name,
			ACL:        aclJSON,
		})
		if err != nil {
			t.Fatalf("CreateDocument(%s): %v", name, err)
		}
		if status != "" && status != "pending" {
			_, err := store.pool.Exec(ctx,
				`UPDATE documents SET status = $1 WHERE id = $2`,
				status, d.ID)
			if err != nil {
				t.Fatalf("UPDATE status: %v", err)
			}
		}
		return d.ID
	}

	docA := mkDoc("docA", userA, "", "done")
	docB := mkDoc("docB", userB, "", "done")
	docShared := mkDoc("docShared", userA, fmt.Sprintf(
		`[{"user_id":"%s"}]`, userB), "done")
	docRole := mkDoc("docRole", userA, `[{"role":"developer"}]`, "done")
	docDeleted := mkDoc("docDeleted", userA, "", "deleted")
	docPending := mkDoc("docPending", userA, "", "pending")

	// Inserts chunks + embeddings.
	// content тестового чанка для NoRawInSnippets: с псевдонимом ФИО_001,
	// БЕЗ raw «Иванов» — иначе симуляция «sanitized» нарушена.
	addChunkWithEmbedding := func(docID string, idx int, content, model string,
		vec []float32) {
		t.Helper()
		var chunkID string
		err := store.pool.QueryRow(ctx,
			`INSERT INTO document_chunks (document_id, chunk_index, content)
			 VALUES ($1, $2, $3) RETURNING id`,
			docID, idx, content).Scan(&chunkID)
		if err != nil {
			t.Fatalf("INSERT chunk: %v", err)
		}
		_, err = store.pool.Exec(ctx,
			`INSERT INTO embeddings (chunk_id, model, dim, embedding)
			 VALUES ($1, $2, $3, $4::vector)`,
			chunkID, model, len(vec), encodeVector(vec))
		if err != nil {
			t.Fatalf("INSERT embedding: %v", err)
		}
	}

	// === DESIGN DECISION: per-test уникальная ось в 1024-векторе ===
	// Не упрощать без причины! Альтернативы хуже:
	//   - TRUNCATE документов перед тестом — ломает `-parallel`;
	//   - схема-namespace per-test — требует миграций (медленно);
	//   - shared close-вектор [1,0,0,...] — вытесняется чужими mock-
	//     документами в общей dev-БД (тесты падают при последовательном
	//     прогоне, см. CLAUDE.md §«Тестовая поллюция dev-БД»).
	// FNV-хэш от per-test prefix даёт O(1) изоляцию в векторном
	// пространстве без блокировок и миграций. Подтверждено архитектором
	// (ревью Ф2 Итерации 11).
	h := fnvHash32(prefix)
	closeAxis := int(h) % 1024
	if closeAxis < 0 {
		closeAxis += 1024
	}
	farAxis := (closeAxis + 1) % 1024
	closeVec := make([]float32, 1024)
	farVec := make([]float32, 1024)
	closeVec[closeAxis] = 1.0
	farVec[farAxis] = 1.0
	for _, doc := range []string{docA, docB, docShared, docRole} {
		addChunkWithEmbedding(doc, 0,
			"close-content для "+doc+" с псевдонимом ФИО_001",
			"mock-sha256-v1", closeVec)
		addChunkWithEmbedding(doc, 1,
			"far-content для "+doc,
			"mock-sha256-v1", farVec)
	}
	// docPending — НЕ имеет embeddings вообще (chunks нет, embeddings нет).
	// docDeleted — даём ему один embedding, чтобы убедиться, что
	// SearchChunks отфильтровывает по status='done'.
	addChunkWithEmbedding(docDeleted, 0,
		"deleted-doc-content", "mock-sha256-v1", closeVec)

	// queryVec ≡ closeVec → top-1 = close-чанк.
	queryVec := make([]float32, 1024)
	copy(queryVec, closeVec)

	return &searchTestCtx{
		store: store,
		userA: userA, userB: userB, adminID: adminID,
		queryVec: queryVec, closeVec: closeVec, farVec: farVec,
		docA: docA, docB: docB, docShared: docShared, docRole: docRole,
		docDeleted: docDeleted, docPending: docPending,
	}
}

// fnvHash32 — простой стабильный hash строки в uint32 (стандартный
// FNV-1a). Используется для выбора per-test «оси» в векторе.
func fnvHash32(s string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime
	}
	return h
}

func TestSearchChunks_OwnerSees(t *testing.T) {
	dsn := requireTestDB(t)
	_ = dsn
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, err := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1", nil, 10)
	if err != nil {
		t.Fatalf("SearchChunks: %v", err)
	}
	if !containsDoc(got, c.docA) {
		t.Errorf("user A не видит свой документ A: %v", docIDs(got))
	}
}

func TestSearchChunks_OtherUserBlind(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	// user B без acl-доступа к docA → не видит docA.
	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userB, "user", "mock-sha256-v1", nil, 10)
	if containsDoc(got, c.docA) {
		t.Errorf("user B видит чужой docA — ACL сломан: %v", docIDs(got))
	}
}

func TestSearchChunks_AdminSeesAll(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	// Используем documentIDs filter — фокусируемся на наших 4 документах,
	// чтобы тест не зависел от поллюции БД (другие mock-документы могут
	// вытеснить наши из top-20).
	myDocs := []string{c.docA, c.docB, c.docShared, c.docRole}
	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.adminID, "admin", "mock-sha256-v1", myDocs, 20)
	for _, d := range myDocs {
		if !containsDoc(got, d) {
			t.Errorf("admin не видит %s", d)
		}
	}
}

func TestSearchChunks_AclUserGrant(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	// user B (роль user) → ACL содержит user_id=userB → видит docShared.
	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userB, "user", "mock-sha256-v1", nil, 10)
	if !containsDoc(got, c.docShared) {
		t.Errorf("user B не видит docShared (acl.user_id): %v", docIDs(got))
	}
}

func TestSearchChunks_AclRoleGrant(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	// user B (роль developer) → ACL содержит role=developer → видит docRole.
	// documentIDs=[docRole] фокусирует поиск (anti-поллюция БД).
	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userB, "developer", "mock-sha256-v1",
		[]string{c.docRole}, 10)
	if !containsDoc(got, c.docRole) {
		t.Errorf("developer не видит docRole (acl.role): %v", docIDs(got))
	}
}

func TestSearchChunks_DeletedDocsExcluded(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1", nil, 20)
	if containsDoc(got, c.docDeleted) {
		t.Errorf("deleted-документ должен быть отфильтрован: %v", docIDs(got))
	}
}

func TestSearchChunks_PendingExcluded(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1", nil, 20)
	if containsDoc(got, c.docPending) {
		t.Errorf("pending-документ без embeddings не должен возвращаться: %v",
			docIDs(got))
	}
}

func TestSearchChunks_LimitClamp(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.adminID, "admin", "mock-sha256-v1", nil, 999)
	if len(got) > 20 {
		t.Errorf("limit > 20 не сработал clamp: got %d", len(got))
	}
}

// TestSearchChunks_EmbedderNameGuard — критический §Р9: запрос с
// embedderName "A" не должен видеть chunks, индексированные embedder'ом "B"
// (иначе cosine ranking бесполезен).
func TestSearchChunks_EmbedderNameGuard(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.adminID, "admin", "non-existent-embedder-name", nil, 10)
	if len(got) != 0 {
		t.Errorf("чужой embedder-name должен дать 0 hits, got %d: %v",
			len(got), docIDs(got))
	}
}

// TestSearchChunks_NoRawInSnippets — regression §Р1: snippet'ы НЕ содержат
// raw данных. Все чанки в setup'е содержат псевдонимы (ФИО_001), не raw.
func TestSearchChunks_NoRawInSnippets(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.adminID, "admin", "mock-sha256-v1", nil, 10)
	for _, r := range got {
		if strings.Contains(r.Snippet, "Иванов") {
			t.Errorf("snippet содержит raw «Иванов»: %q (chunk %s)",
				r.Snippet, r.ChunkID)
		}
	}
}

// TestSearchChunks_DocumentIdsCannotBypassACL — BLOCKER B1.
// User B передаёт docA (чужой) явно в фильтре → 0 hits (silent),
// никаких 403/500, ACL предикат отфильтрует.
func TestSearchChunks_DocumentIdsCannotBypassACL(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, err := store.SearchChunks(context.Background(), c.queryVec,
		c.userB, "user", "mock-sha256-v1",
		[]string{c.docA}, 10)
	if err != nil {
		t.Fatalf("SearchChunks с чужим document_id вернул ошибку (а должен silent 0): %v", err)
	}
	if containsDoc(got, c.docA) {
		t.Errorf("BLOCKER B1 ACL bypass: user B получил docA через document_ids: %v",
			docIDs(got))
	}
	if len(got) != 0 {
		t.Errorf("ожидался 0 hits (чужой doc + userB не в acl), got %d: %v",
			len(got), docIDs(got))
	}
}

// TestSearchChunks_DocumentIdsCombinesWithACL — фильтр documentIDs работает
// как AND поверх ACL: user A передаёт [docA, docB] → видит только docA
// (docB чужой и не в acl).
func TestSearchChunks_DocumentIdsCombinesWithACL(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1",
		[]string{c.docA, c.docB}, 10)
	if !containsDoc(got, c.docA) {
		t.Errorf("свой docA не виден при documentIDs filter: %v", docIDs(got))
	}
	if containsDoc(got, c.docB) {
		t.Errorf("чужой docB виден через documentIDs filter (BLOCKER B1): %v",
			docIDs(got))
	}
}

// TestSearchChunks_DocumentIdsBypassFuzz — fuzz-style регрессионный
// тест: 20 случайных uuid + чужие docID в documentIDs от user без прав
// → ВСЕГДА 0 hits. Защита от регрессии BLOCKER B1: если кто-то
// «оптимизирует» SQL (например, заменит ACL+documentIDs на
// `c.document_id IN ($docs)`), этот тест поймает.
func TestSearchChunks_DocumentIdsBypassFuzz(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	// user B (роль user) НЕ имеет доступа к docA/docRole/docDeleted/docPending.
	// Передаём 20 случайных uuid + docA + docRole → ACL должен отрезать всё.
	foreignIDs := make([]string, 0, 22)
	for i := 0; i < 20; i++ {
		foreignIDs = append(foreignIDs, randomUUIDish(t, i))
	}
	foreignIDs = append(foreignIDs, c.docA, c.docRole)

	got, err := store.SearchChunks(context.Background(), c.queryVec,
		c.userB, "user", "mock-sha256-v1", foreignIDs, 20)
	if err != nil {
		t.Fatalf("SearchChunks fuzz: %v", err)
	}
	for _, r := range got {
		if r.DocumentID == c.docA || r.DocumentID == c.docRole {
			t.Errorf("BLOCKER B1 регрессия: чужой docID %s прошёл через documentIDs",
				r.DocumentID)
		}
	}
}

// TestSearchChunks_EmbedderNameRequired — fail-closed §Р9: пустой
// embedderName → ErrEmbedderNameRequired (никаких silent-результатов
// из чужого векторного пространства).
func TestSearchChunks_EmbedderNameRequired(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	_, err := store.SearchChunks(context.Background(), c.queryVec,
		c.adminID, "admin", "", nil, 10)
	if err == nil {
		t.Fatal("ожидалась ErrEmbedderNameRequired при пустом embedderName")
	}
	if !errors.Is(err, ErrEmbedderNameRequired) {
		t.Errorf("ожидалась ErrEmbedderNameRequired, got %v", err)
	}
}

// randomUUIDish — генерирует строку формата uuid (детерминированно от seed)
// для fuzz-теста. Для теста ACL-фильтра достаточно правильного синтаксиса
// `ANY($N::uuid[])`; значение не должно ссылаться на реальный документ.
func randomUUIDish(t *testing.T, seed int) string {
	t.Helper()
	h := fnvHash32(fmt.Sprintf("fuzz_%s_%d", t.Name(), seed))
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		h, uint16(h)^0xa1b2, uint16(h)^0xc3d4,
		uint16(h)^0xe5f6, uint64(h)*0x123)
}

// TestSearchChunks_DocumentIdsRestrictsToSubset — owner документов A и shared,
// document_ids=[docShared] → видит только docShared.
func TestSearchChunks_DocumentIdsRestrictsToSubset(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1",
		[]string{c.docShared}, 10)
	for _, r := range got {
		if r.DocumentID != c.docShared {
			t.Errorf("documentIDs=[docShared] вернул %s: %v",
				r.DocumentID, docIDs(got))
		}
	}
}

// TestSearchChunks_RankingByCosine — closeVec идентичен queryVec
// (cosine=1, distance=0, relevance=1); farVec ортогонален queryVec
// (cosine=0, distance=1, relevance=0). Все chunk'и с idx=0 (close)
// должны быть выше chunk'ов с idx=1 (far). Фильтруем по docA, чтобы
// не зависеть от поллюции БД.
func TestSearchChunks_RankingByCosine(t *testing.T) {
	requireTestDB(t)
	store := testStore(t)
	defer store.Close()
	c := setupSearchTest(t, store)

	got, _ := store.SearchChunks(context.Background(), c.queryVec,
		c.userA, "user", "mock-sha256-v1",
		[]string{c.docA}, 20)
	if len(got) != 2 {
		t.Fatalf("ожидалось ровно 2 чанка docA, got %d: %+v", len(got), got)
	}
	// Первый результат должен иметь chunk_index=0 (closeVec).
	if got[0].ChunkIndex != 0 {
		t.Errorf("top-1 не close-chunk: chunk_index=%d, relevance=%v",
			got[0].ChunkIndex, got[0].Relevance)
	}
	// relevance close > relevance far (cosine 1 vs 0).
	if got[0].Relevance <= got[1].Relevance {
		t.Errorf("ranking сломан: close relevance=%v, far relevance=%v",
			got[0].Relevance, got[1].Relevance)
	}
}

// --- helpers ---

func requireTestDB(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционный тест БД пропущен")
	}
	return dsn
}

func containsDoc(rs []SearchResult, id string) bool {
	for _, r := range rs {
		if r.DocumentID == id {
			return true
		}
	}
	return false
}

func docIDs(rs []SearchResult) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.DocumentID)
	}
	return out
}

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeEmbeddingVector — генератор «реалистичного» ответа.
func makeEmbeddingVector(dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(i%200-100) / 100.0
	}
	return v
}

func TestOpenAIEmbedderSendsCorrectRequest(t *testing.T) {
	var gotBody map[string]any
	var gotPath, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": makeEmbeddingVector(EmbeddingDim)}},
		})
	}))
	defer srv.Close()

	e := NewOpenAICompatibleEmbedder(srv.URL, "bge-m3", "sk-secret", 5*time.Second)
	if _, err := e.Embed(context.Background(), "hello"); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("path = %q, ожидался /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotBody["model"] != "bge-m3" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["input"] != "hello" {
		t.Errorf("input = %v", gotBody["input"])
	}
}

func TestOpenAIEmbedderNoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": makeEmbeddingVector(EmbeddingDim)}},
		})
	}))
	defer srv.Close()

	e := NewOpenAICompatibleEmbedder(srv.URL, "bge-m3", "", 5*time.Second)
	if _, err := e.Embed(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("при пустом ключе Authorization не должен слаться, got %q", gotAuth)
	}
}

func TestOpenAIEmbedderParsesEmbedding(t *testing.T) {
	want := makeEmbeddingVector(EmbeddingDim)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": want}},
		})
	}))
	defer srv.Close()

	e := NewOpenAICompatibleEmbedder(srv.URL, "bge-m3", "k", 5*time.Second)
	got, err := e.Embed(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != EmbeddingDim {
		t.Fatalf("dim = %d", len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("mismatch на индексе %d: %v != %v", i, got[i], want[i])
			return
		}
	}
}

// TestOpenAIEmbedderFailsClosedOnDimMismatch — критический инвариант
// (план §Р2): провайдер ОБЯЗАН вернуть ошибку при dim ≠ 1024, чтобы
// не сломать схему БД vector(1024).
func TestOpenAIEmbedderFailsClosedOnDimMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": makeEmbeddingVector(512)}},
		})
	}))
	defer srv.Close()
	e := NewOpenAICompatibleEmbedder(srv.URL, "bge-m3", "k", 5*time.Second)
	_, err := e.Embed(context.Background(), "x")
	if err == nil {
		t.Fatal("ожидалась ошибка fail-closed на dim mismatch")
	}
	if !strings.Contains(err.Error(), "dim") {
		t.Errorf("ошибка должна упоминать dim, got %v", err)
	}
}

func TestOpenAIEmbedderFailsOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	e := NewOpenAICompatibleEmbedder(srv.URL, "m", "k", 2*time.Second)
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("ожидалась ошибка на 5xx")
	}
}

func TestOpenAIEmbedderFailsOnEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()
	e := NewOpenAICompatibleEmbedder(srv.URL, "m", "k", 2*time.Second)
	if _, err := e.Embed(context.Background(), "x"); err == nil {
		t.Fatal("ожидалась ошибка на пустой data")
	}
}

func TestOpenAIEmbedderRespectsContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	e := NewOpenAICompatibleEmbedder(srv.URL, "m", "k", 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := e.Embed(ctx, "x")
	if err == nil {
		t.Fatal("ожидалась ошибка по cancel")
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "context") {
		t.Errorf("ожидалась ошибка context, got %v", err)
	}
}

func TestOpenAIEmbedderNameAndDim(t *testing.T) {
	e := NewOpenAICompatibleEmbedder("http://x", "bge-m3", "k", time.Second)
	if e.Name() != "bge-m3" {
		t.Errorf("name = %q", e.Name())
	}
	if e.Dim() != EmbeddingDim {
		t.Errorf("dim = %d", e.Dim())
	}
}

// TestOpenAIEmbedderImplementsInterface — compile-time guard.
func TestOpenAIEmbedderImplementsInterface(t *testing.T) {
	var _ Embedder = NewOpenAICompatibleEmbedder("http://x", "m", "k", time.Second)
}

// TestOpenAIEmbedderEndpointWithTrailingSlash — URL c trailing slash
// и без должны давать одинаковый результирующий запрос /v1/embeddings.
func TestOpenAIEmbedderEndpointWithTrailingSlash(t *testing.T) {
	for _, base := range []string{"http://example.com", "http://example.com/"} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{"embedding": makeEmbeddingVector(EmbeddingDim)}},
			})
		}))
		// Подменяем base на адрес тестового сервера.
		e := NewOpenAICompatibleEmbedder(strings.Replace(base,
			"http://example.com", srv.URL, 1), "m", "k", 2*time.Second)
		if _, err := e.Embed(context.Background(), "x"); err != nil {
			t.Fatalf("base=%q: %v", base, err)
		}
		if gotPath != "/v1/embeddings" {
			t.Errorf("base=%q → path=%q", base, gotPath)
		}
		srv.Close()
	}
}

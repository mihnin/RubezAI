package llm

import (
	"context"
	"testing"
)

func TestMockEmbedderDeterministic(t *testing.T) {
	e := MockEmbedder{}
	v1, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	v2, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v1) != EmbeddingDim {
		t.Fatalf("dim = %d, ожидалось %d", len(v1), EmbeddingDim)
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Errorf("не детерминирован на индексе %d: %v != %v", i, v1[i], v2[i])
			return
		}
	}
}

func TestMockEmbedderDifferentInputs(t *testing.T) {
	e := MockEmbedder{}
	v1, _ := e.Embed(context.Background(), "query A")
	v2, _ := e.Embed(context.Background(), "query B")
	same := 0
	for i := range v1 {
		if v1[i] == v2[i] {
			same++
		}
	}
	// Допускаем редкие совпадения (детерминизм SHA-256), но не все.
	if same > EmbeddingDim/4 {
		t.Errorf("слишком много совпадающих компонент: %d/%d", same, EmbeddingDim)
	}
}

func TestMockEmbedderRange(t *testing.T) {
	v, _ := MockEmbedder{}.Embed(context.Background(), "range check")
	for i, f := range v {
		if f < -1.0 || f > 1.0 {
			t.Errorf("компонента %d вне [-1,1]: %v", i, f)
		}
	}
}

func TestMockEmbedderName(t *testing.T) {
	if n := (MockEmbedder{}).Name(); n != "mock-sha256-v1" {
		t.Errorf("name = %q", n)
	}
}

func TestMockEmbedderDim(t *testing.T) {
	if d := (MockEmbedder{}).Dim(); d != EmbeddingDim {
		t.Errorf("dim = %d, ожидалось %d", d, EmbeddingDim)
	}
}

// TestMockEmbedderImplementsInterface — статическая проверка через
// присваивание интерфейса (compile-time guard).
func TestMockEmbedderImplementsInterface(t *testing.T) {
	var _ Embedder = MockEmbedder{}
}

package llm

import "testing"

func TestMockEmbedderDeterministic(t *testing.T) {
	e := MockEmbedder{}
	v1 := e.Embed("hello world")
	v2 := e.Embed("hello world")
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
	v1 := e.Embed("query A")
	v2 := e.Embed("query B")
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
	v := MockEmbedder{}.Embed("range check")
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

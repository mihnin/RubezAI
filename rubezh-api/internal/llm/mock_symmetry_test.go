package llm

import (
	"context"
	"testing"
)

// goldenMockHelloFirst16 — первые 16 компонент MockEmbedder.Embed("hello").
// КРИТИЧНО: эта константа ОБЯЗАНА совпадать байт-в-байт с Python-выводом
// `MockEmbedder().embed("hello")[0:16]` (rubezh-worker/app/embeddings/mock.py).
// Если эти числа меняются — symmetry между worker (Python embed)
// и API (Go embed) нарушена; query-вектор и doc-вектор живут в разных
// пространствах; cosine-ranking бесполезен.
//
// План §Р2: «MockEmbedder бинарно совместим в обоих языках».
// Проверка дополняется integration-тестом Python→Go в Ф4.
// Источник: rubezh-worker/app/embeddings/mock.py с одинаковым алгоритмом
// (см. также tests/test_embeddings.py::test_golden_for_hello). Если
// этот вектор меняется — обязательно синхронизировать обе стороны.
var goldenMockHelloFirst16 = []float32{
	0.2631225130, -0.5483201705, -0.0798793016, -0.0238901642,
	-0.7237447212, 0.7819415610, 0.9577826294, 0.4705865914,
	0.8632575544, 0.8213765537, 0.1766301035, 0.3797996584,
	0.0702516143, -0.8193402956, -0.4320218465, 0.2311942987,
}

// TestMockEmbedderGoldenForHello — symmetry guard. Если этот тест
// падает, проверь sync'ность алгоритма с Python:
// - encoding текста ("hello#0".encode());
// - порядок байтов в SHA-256 (BigEndian uint32);
// - формула нормализации: val/2^32 * 2 - 1.
func TestMockEmbedderGoldenForHello(t *testing.T) {
	v, err := MockEmbedder{}.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) < len(goldenMockHelloFirst16) {
		t.Fatalf("вектор короче golden: %d < %d", len(v), len(goldenMockHelloFirst16))
	}
	const eps = 1e-6
	for i, want := range goldenMockHelloFirst16 {
		got := v[i]
		diff := got - want
		if diff < 0 {
			diff = -diff
		}
		if diff > eps {
			t.Errorf("симметрия Go↔Python сломана на индексе %d: got=%v want=%v (diff=%v)",
				i, got, want, diff)
		}
	}
}

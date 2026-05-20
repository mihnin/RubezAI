package llm

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// EmbeddingDim — размерность векторов embeddings (фикс схема
// embeddings.vector(1024)).
const EmbeddingDim = 1024

// MockEmbedder — детерминированный embedder через SHA-256.
// Аналог Python-реализации в rubezh-worker для совместимости
// «query-embed == doc-embed» (Итерация 11 RAG).
type MockEmbedder struct{}

// Name возвращает идентификатор модели для embeddings.model колонки.
func (MockEmbedder) Name() string { return "mock-sha256-v1" }

// Embed возвращает 1024-мерный детерминированный вектор для текста.
// Алгоритм идентичен Python-MockEmbedder.embed (counter-mode SHA-256).
func (MockEmbedder) Embed(text string) []float32 {
	out := make([]float32, 0, EmbeddingDim)
	counter := 0
	for len(out) < EmbeddingDim {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", text, counter)))
		for i := 0; i+4 <= len(h); i += 4 {
			if len(out) >= EmbeddingDim {
				break
			}
			val := float32(binary.BigEndian.Uint32(h[i:i+4])) / 4294967295.0
			out = append(out, val*2-1) // [-1, 1]
		}
		counter++
	}
	return out
}

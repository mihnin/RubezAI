package llm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// EmbeddingDim — размерность векторов embeddings (фикс схема
// embeddings.vector(1024)). Любой Embedder ОБЯЗАН возвращать вектор
// именно этой длины — иначе SearchChunks/InsertEmbedding фейлятся
// на уровне БД (vector dim mismatch).
const EmbeddingDim = 1024

// Embedder — точка расширения для embedder-провайдеров.
// Реализации (Итерация 11, план iteration-11-rag.md §Р2):
//   - MockEmbedder              — детерминированный SHA-256, default;
//   - OpenAICompatibleEmbedder  — POST /v1/embeddings (LM Studio,
//     vLLM, Ollama, любая OpenAI-совместимая embedding-модель).
//
// Симметрия Go↔Python: worker (Python) и API (Go) используют ОДИН
// embedder (env EMBEDDER_KIND, EMBEDDER_URL, EMBEDDER_MODEL шарятся).
// Иначе query-вектор и doc-вектор живут в разных пространствах.
// Защита от смены embedder'а на runtime — `embeddings.model` колонка +
// embedder-name guard в `SearchChunks` (план §Р9).
type Embedder interface {
	// Embed возвращает вектор длины Dim() для текста. ctx — для
	// HTTP-провайдеров (deadline, cancel); MockEmbedder его игнорирует.
	// Реализация ОБЯЗАНА fail-closed (вернуть ошибку), если вектор
	// получился не Dim() длины — иначе порча БД.
	Embed(ctx context.Context, text string) ([]float32, error)

	// Name — идентификатор embedder'а для колонки `embeddings.model`.
	// Должен быть стабилен между Embed-вызовами одной сессии и
	// идентичен между Go и Python для симметрии.
	Name() string

	// Dim — размерность возвращаемого вектора. Всегда EmbeddingDim (1024)
	// для текущей схемы; вынесено в метод как контрактный гарант.
	Dim() int
}

// MockEmbedder — детерминированный embedder через SHA-256.
// Аналог Python-реализации в rubezh-worker/app/embeddings/mock.py для
// симметрии «query-embed == doc-embed» (Итерация 11 RAG).
type MockEmbedder struct{}

// Name возвращает идентификатор модели для embeddings.model колонки.
func (MockEmbedder) Name() string { return "mock-sha256-v1" }

// Dim возвращает фиксированную размерность 1024.
func (MockEmbedder) Dim() int { return EmbeddingDim }

// Embed возвращает 1024-мерный детерминированный вектор для текста.
// Алгоритм бинарно идентичен Python-MockEmbedder.embed (counter-mode
// SHA-256) — Итерация 11 §Р2:
//
//   - один SHA-256 от `"{text}#{counter}"` (UTF-8) даёт 8 значений
//     (32 байта / 4 = 8 uint32);
//   - каждое uint32 нормируется делением на 2^32 (4294967296.0) —
//     совпадает с Python `int.from_bytes(...,"big") / 2**32`;
//   - линейный shift в [-1, 1]: val*2 - 1;
//   - counter инкрементируется до набора EmbeddingDim значений.
//
// КРИТИЧЕСКОЕ ОТЛИЧИЕ ОТ ПРЕЖНЕЙ Ф0-РЕАЛИЗАЦИИ: использовался делитель
// `4294967295.0` (2^32 - 1) — расходился с Python (`2**32`). Это
// нарушало cross-language симметрию: query Go-эмбедил с одним
// делителем, документы worker Python-эмбедил с другим — векторы
// жили в разных пространствах, cosine-ranking был бесполезен.
// Багу поймал TestMockEmbedderGoldenForHello (Ф1 Итерации 11).
//
// Никогда не возвращает ошибку (детерминированный, без I/O), но
// сигнатура совместима с Embedder для DI.
func (MockEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	out := make([]float32, 0, EmbeddingDim)
	counter := 0
	for len(out) < EmbeddingDim {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s#%d", text, counter)))
		for i := 0; i+4 <= len(h); i += 4 {
			if len(out) >= EmbeddingDim {
				break
			}
			val := float32(binary.BigEndian.Uint32(h[i:i+4])) / 4294967296.0
			out = append(out, val*2-1) // [-1, 1]
		}
		counter++
	}
	return out, nil
}

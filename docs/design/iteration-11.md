# Итерация 11 — Базовый RAG с ACL по pgvector

> **СТАТУС: SUPERSEDED.** Этот документ — v1, частично реализованный (Ф0:
> `storage/search.go`, `api/search.go`, mock-embedder, миграция HNSW,
> `search_performed` в audit-enum). Полный план реализации с дельтой
> поверх Ф0 — `docs/design/iteration-11-rag.md` (v2). Не используйте v1
> для планирования новых работ.

Архитектурный план **v1**. Закрывает критерий MVP «поиск по
документам» (продолжение Итерации 10). Минимальная реализация
для MVP — векторный поиск с ACL-фильтрацией.

## 1. Цель и границы

### Эндпойнт
- `POST /api/search` — body `{query: string, limit?: int}`.
  Возвращает топ-N релевантных чанков с метаданными.

### Pipeline
1. Sanitize query через `rubezh-sanitizer` (как в чате) — query может
   содержать ПДн.
2. Embed sanitized query через `MockEmbedder` (тот же что Worker).
   Размерность 1024.
3. Поиск через `embeddings <=> query_vector` (cosine distance).
4. JOIN `document_chunks` + `documents` + ACL-фильтр.
5. Возврат списка с `document_id`, `chunk_index`, `content`,
   `relevance` (1 - cosine_distance), метаданные документа.

### Безопасность
- ACL-фильтрация **в SQL** (тот же `acl @>` что в `ListDocuments`):
  только документы, к которым у пользователя есть доступ.
- Embeddings и chunks хранят **sanitized content** (план Итерации 10
  §Р4), поэтому ответ RAG не отдаёт raw PII.
- Audit-event `search_performed` с `query_hash` (не plaintext!) и
  `result_count`.

### Вне границ
- **Реальный векторизатор** — пост-MVP. Mock-embeddings дают
  «бессмысленные» результаты семантически, но pipeline валидируется.
- **Reranking** — после MVP.
- **Streaming results** — после MVP.

## 2. Реализация

### Go-storage

```go
// internal/storage/search.go
func (s *Storage) SearchChunks(ctx, queryVector []float32,
                                userID, role string, limit int) ([]SearchResult, error)
```

SQL:
```sql
SELECT c.id, c.document_id, c.chunk_index, c.content, c.token_count,
       d.filename,
       1 - (e.embedding <=> $1::vector) AS relevance
FROM embeddings e
JOIN document_chunks c ON c.id = e.chunk_id
JOIN documents d ON d.id = c.document_id
WHERE d.status = 'done'
  AND (<ACL-условие — copy из ListDocuments>)
ORDER BY e.embedding <=> $1::vector
LIMIT $2
```

### Go-API

```go
// internal/api/search.go
POST /api/search → sanitize → embed → SearchChunks → DTO.
```

### Embedder в Go

Worker'овский MockEmbedder в Python. Для API нужен Go-эквивалент:
тот же SHA-256-based детерминированный mock в `internal/llm/embedder.go`.

### Audit

`search_performed` event:
- `detail.query_hash = SHA-256(plaintext)[:16]` hex (для корреляции
  без plaintext);
- `detail.result_count`;
- `detail.has_sanitized_pii: bool` (флаг — query содержал
  обезличенные сущности).

## 3. Тесты

- ACL: user видит только свои или role-разрешённые документы.
- Sanitize в pipeline: query «Иванов» → не падает.
- Empty results: нет документов → пустой массив.
- Limit: соблюдается; max=20.

## 4. Самооценка плана: 9/10

Mock-embeddings дают семантически бессмысленные результаты, но MVP-цель —
продемонстрировать pipeline. Без реального векторизатора оценка ≥9
не возможна архитектурно; принимаю как ограничение MVP.

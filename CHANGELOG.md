# Changelog — Рубеж ИИ

Документирует **breaking changes** и значимые изменения архитектуры/контрактов
между итерациями. Цель — чтобы dev-окружения, существовавшие до изменения,
могли мигрировать без слепого дебага.

Формат вдохновлён [Keep a Changelog](https://keepachangelog.com/). Раздел
«Breaking» — обязателен для любой правки, ломающей сериализацию данных,
контракты или векторное пространство embeddings.

## [Unreleased]

### Breaking

#### `MockEmbedder` — векторное пространство изменилось

**Что:** В `internal/llm/embedder.go::MockEmbedder.Embed` делитель нормализации
изменён с `4294967295.0` (2^32 - 1) на `4294967296.0` (2^32). Это совпадает с
формулой Python-эквивалента `rubezh-worker/app/embeddings/mock.py` —
`int.from_bytes(...,"big") / 2**32`.

**Почему:** До этой правки Go и Python давали **разные** векторы для одного
и того же входа — крайне маленькая, но систематическая разница, которая
ломала cross-language symmetry (план Итерации 11 §Р2). В результате
worker (doc-embed Python) и API (query-embed Go) жили в **разных векторных
пространствах**, и cosine ranking в `SearchChunks` был бесполезен.
Баг поймал TDD-тест `TestMockEmbedderGoldenForHello` (Ф1 Итерации 11).

**Влияние на прод:** **Нулевое.** RAG ещё не выкачен в прод, MockEmbedder
использовался только в тестах и dev-окружениях. Production-индексы не
существуют.

**Влияние на dev-окружения:** Если в dev-БД уже есть записи в `embeddings`
от старого MockEmbedder, они **не совместимы** с новым кодом. Действия:

```sql
-- Удалить старые embeddings (worker переиндексирует автоматически при
-- следующем claim'е документа):
DELETE FROM embeddings WHERE model = 'mock-sha256-v1';

-- ИЛИ: пересоздать БД целиком:
docker compose down -v && docker compose up -d --build
```

Симметрия гарантирована тестами `TestMockEmbedderGoldenForHello` (Go) и
`test_mock_embedder_golden_for_hello` (Python) — оба сравнивают первые
16 компонент с одной и той же золотой константой; pre-commit падает на
расхождении.

### Added

- Итерация 11 Ф1: интерфейс `llm.Embedder` (`Embed`, `Name`, `Dim`).
- Итерация 11 Ф1: `llm.OpenAICompatibleEmbedder` (POST `/v1/embeddings`,
  fail-closed на dim ≠ 1024, на 5xx, на пустой data). Зеркальная Python-
  реализация в `rubezh_worker.app.embeddings.openai_compatible`.
- Итерация 11 Ф1: env `EMBEDDER_KIND` (`mock` | `openai_compatible`),
  `EMBEDDER_URL`, `EMBEDDER_MODEL`, `EMBEDDER_API_KEY`,
  `EMBEDDER_TIMEOUT_SECONDS`. См. `.env.example`.
- Итерация 11 Ф1: фабрики `cmd/rubezh-api/main.go::buildEmbedder` и
  `app.embeddings.build_embedder` (fail-closed на missing URL/Model
  для `openai_compatible`).
- Техдолг: пакет `internal/testdb` с пост-прогонным cleanup'ом
  интеграционных тестов (per-pid префикс, host-allowlist, env-override
  `TESTDB_ALLOW_HOST`). См. CLAUDE.md §«Особенности окружения».

### Changed

- `api.Deps.Embedder` теперь **обязательное** поле: `NewRouter` panic'ает
  при nil (Итерация 11 §Р2: запрет тихих деградаций к mock-embedder'у
  при misconfiguration). Тесты должны явно передавать
  `Embedder: llm.MockEmbedder{}`.
- `llm.MockEmbedder.Embed` сигнатура изменилась с `Embed(text) []float32`
  на `Embed(ctx, text) ([]float32, error)` (соответствие интерфейсу
  `llm.Embedder`).

### Closed (техдолг)

- Поллюция dev-БД интеграционными тестами: реализован пакет
  `internal/testdb` с пост-прогонным cleanup'ом по per-pid префиксу
  (`itest_<pid>_`), защита от prod-БД через host-allowlist + env-override
  `TESTDB_ALLOW_HOST`.

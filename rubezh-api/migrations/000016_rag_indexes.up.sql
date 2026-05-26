-- Итерация 11 Ф2: композитный индекс для correlated subquery в SearchChunks.
-- search.go получает risk_level через подзапрос:
--   SELECT risk_level FROM sanitization_results sr
--   WHERE sr.document_id = d.id
--   ORDER BY created_at DESC LIMIT 1
-- Без составного индекса (document_id, created_at DESC) подзапрос
-- деградирует в Index Scan + Sort. С индексом — Index Only Scan,
-- O(log N) на документ × top-K → ≈ 20 lookups для дефолтного limit=10.

CREATE INDEX IF NOT EXISTS idx_sanitization_results_doc_created
  ON sanitization_results (document_id, created_at DESC);

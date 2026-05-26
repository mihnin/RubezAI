package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SnippetMaxRunes — длина snippet'а в результатах SearchChunks
// (план Итерации 11 §Р3). Truncate по rune boundary (не байту), чтобы
// не побить UTF-8 (особенно кириллицу). Полный chunk доступен через
// `GET /api/documents/:id/chunks` если он понадобится UI.
const SnippetMaxRunes = 512

// SearchResult — строка результата RAG (Итерация 11).
type SearchResult struct {
	ChunkID    string
	DocumentID string
	ChunkIndex int
	Snippet    string // первые SnippetMaxRunes рун sanitized content
	Filename   string
	Relevance  float64 // 1 - cosine_distance; ≥0 — лучше
	RiskLevel  *string // low|medium|high|critical|nil (нет sanitization_results)
}

// ErrEmbedderNameRequired — SearchChunks вызван с пустым embedderName.
// Это fail-closed гарантия: в production без embedderName cosine-ranking
// бесполезен (запрос мог бы вытащить чанки из чужого векторного
// пространства). Тесты ОБЯЗАНЫ передавать имя embedder'а явно.
var ErrEmbedderNameRequired = errors.New(
	"storage: embedderName обязателен для cosine-сравнимости (план §Р9)")

// SearchChunks — векторный поиск по embeddings с ACL-фильтрацией.
//
// queryVector — list[float32] длины 1024 (фикс схема embeddings.vector(1024)).
// embedderName — ОБЯЗАТЕЛЕН (план §Р9): фильтрует `e.model = $embedderName`,
// гарантируя cosine-сравнимость с doc-векторами. Пустое значение →
// ErrEmbedderNameRequired (fail-closed; никаких silent fallback на «все
// embedder'ы», иначе ranking ломается смешиванием векторных пространств).
// documentIDs — опциональный фильтр поверх ACL (план §Р3): применяется
// КАК `AND c.document_id = ANY($N::uuid[])`, а не вместо ACL (BLOCKER B1).
// Пустой/nil — без фильтра.
//
// ACL-инвариант (BLOCKER B1, план §Р3): supervisor-роли (admin /
// security_officer / compliance_officer / auditor) видят все документы;
// остальные видят только owner_id == userID OR acl содержит user_id ==
// userID OR acl содержит role == role. documentIDs филтр НЕ ослабляет
// ACL — чужой document_id силён в фильтре, но фильтрация ACL остаётся
// в WHERE → результат пуст (silent, не 403).
func (s *Storage) SearchChunks(
	ctx context.Context, queryVector []float32,
	userID, role, embedderName string,
	documentIDs []string, limit int,
) ([]SearchResult, error) {
	if embedderName == "" {
		return nil, ErrEmbedderNameRequired
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	vecLit := encodeVector(queryVector)

	conds := []string{"d.status = 'done'"}
	args := []any{vecLit, embedderName}
	conds = append(conds, "e.model = $2")

	// ACL — ВСЕГДА первый и обязательный для не-supervisor (BLOCKER B1).
	// Никакой документIDs filter ниже не может ослабить этот предикат.
	switch role {
	case "admin", "security_officer", "compliance_officer", "auditor":
		// supervisor-roles — без ACL-фильтра.
	default:
		args = append(args, userID, role)
		conds = append(conds, fmt.Sprintf(
			`(d.owner_id = $%d::uuid
			   OR d.acl @> jsonb_build_array(jsonb_build_object('user_id', $%d::text))
			   OR d.acl @> jsonb_build_array(jsonb_build_object('role', $%d::text)))`,
			len(args)-1, len(args)-1, len(args)))
	}

	// documentIDs filter — ПОВЕРХ ACL (AND, не OR). Если в documentIDs
	// чужой id — ACL-предикат уже отрежет соответствующие чанки → 0 hits.
	if len(documentIDs) > 0 {
		args = append(args, documentIDs)
		conds = append(conds, fmt.Sprintf(
			"c.document_id = ANY($%d::uuid[])", len(args)))
	}

	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT c.id, c.document_id, c.chunk_index, c.content,
		       d.filename, 1 - (e.embedding <=> $1::vector) AS relevance,
		       (SELECT risk_level FROM sanitization_results sr
		        WHERE sr.document_id = d.id
		        ORDER BY created_at DESC LIMIT 1) AS risk_level
		FROM embeddings e
		JOIN document_chunks c ON c.id = e.chunk_id
		JOIN documents d ON d.id = c.document_id
		WHERE %s
		ORDER BY e.embedding <=> $1::vector
		LIMIT $%d
	`, strings.Join(conds, " AND "), len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: search: %w", err)
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		var content string
		if err := rows.Scan(&r.ChunkID, &r.DocumentID, &r.ChunkIndex,
			&content, &r.Filename, &r.Relevance, &r.RiskLevel); err != nil {
			return nil, fmt.Errorf("storage: scan search: %w", err)
		}
		r.Snippet = truncateRunes(content, SnippetMaxRunes)
		out = append(out, r)
	}
	return out, rows.Err()
}

// truncateRunes возвращает первые `maxRunes` рун строки. UTF-8-safe:
// не разрезает многобайтовые символы (кириллица, эмодзи) пополам.
// Если строка короче — возвращается без изменений.
func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	i, count := 0, 0
	for i < len(s) && count < maxRunes {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	return s[:i]
}

// encodeVector — преобразует []float32 в pgvector literal формата "[v1,v2,...]".
func encodeVector(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

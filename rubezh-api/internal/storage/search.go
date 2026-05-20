package storage

import (
	"context"
	"fmt"
	"strings"
)

// SearchResult — строка результата RAG (Итерация 11).
type SearchResult struct {
	ChunkID    string
	DocumentID string
	ChunkIndex int
	Content    string // sanitized (план Итерации 10 §Р4)
	Filename   string
	Relevance  float64 // 1 - cosine_distance; ≥0 — лучше
}

// SearchChunks — векторный поиск по embeddings с ACL-фильтрацией.
// queryVector — list[float32] длины 1024 (фикс схема).
func (s *Storage) SearchChunks(
	ctx context.Context, queryVector []float32,
	userID, role string, limit int,
) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	vecLit := encodeVector(queryVector)

	conds := []string{"d.status = 'done'"}
	args := []any{vecLit}
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
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT c.id, c.document_id, c.chunk_index, c.content,
		       d.filename, 1 - (e.embedding <=> $1::vector) AS relevance
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
		if err := rows.Scan(&r.ChunkID, &r.DocumentID, &r.ChunkIndex,
			&r.Content, &r.Filename, &r.Relevance); err != nil {
			return nil, fmt.Errorf("storage: scan search: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// encodeVector — преобразует []float32 в pgvector literal формата "[v1,v2,...]".
func encodeVector(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

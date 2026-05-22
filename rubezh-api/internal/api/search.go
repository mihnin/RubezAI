package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

type searchRequestDTO struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type searchResultDTO struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Content    string  `json:"content"` // sanitized
	Filename   string  `json:"filename"`
	Relevance  float64 `json:"relevance"`
}

type searchResponseDTO struct {
	Results []searchResultDTO `json:"results"`
}

// searchHandler — POST /api/search.
// Pipeline: sanitize query → embed → SearchChunks (с ACL).
func searchHandler(
	store *storage.Storage, sanClient *sanitizer.Client,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var dto searchRequestDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if dto.Query == "" {
			http.Error(w, "query обязателен", http.StatusBadRequest)
			return
		}
		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())

		// Sanitize query (может содержать ПДн).
		sanitizedQuery := dto.Query
		hasPII := false
		if sanClient != nil {
			res, err := sanClient.Preview(r.Context(),
				sanitizer.PreviewRequest{Text: dto.Query, Context: "chat"})
			if err == nil {
				sanitizedQuery = res.SanitizedText
				hasPII = len(res.Entities) > 0
			}
		}

		// Embed sanitized query — тот же MockEmbedder что worker.
		queryVec := llm.MockEmbedder{}.Embed(sanitizedQuery)

		// Поиск.
		results, err := store.SearchChunks(r.Context(), queryVec,
			userID, string(role), dto.Limit)
		if err != nil {
			http.Error(w, "ошибка поиска",
				http.StatusInternalServerError)
			return
		}

		// Audit (query_hash без plaintext).
		queryHash := sha256.Sum256([]byte(dto.Query))
		_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
			UserID: userID, EventType: "search_performed",
			Detail: map[string]any{
				"query_hash":        hex.EncodeToString(queryHash[:8]),
				"result_count":      len(results),
				"has_sanitized_pii": hasPII,
				"sanitized_only":    sanClient != nil,
			},
		})

		out := make([]searchResultDTO, 0, len(results))
		for _, r := range results {
			out = append(out, searchResultDTO{
				ChunkID: r.ChunkID, DocumentID: r.DocumentID,
				ChunkIndex: r.ChunkIndex, Content: r.Content,
				Filename: r.Filename, Relevance: r.Relevance,
			})
		}
		writeJSON(w, http.StatusOK, searchResponseDTO{Results: out})
	}
}

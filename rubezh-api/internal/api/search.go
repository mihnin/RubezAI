package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/rubezh-ai/rubezh-api/internal/auth"
	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// runeCount — кол-во рун в строке (для лимитов запросов).
func runeCount(s string) int { return utf8.RuneCountInString(s) }

// SearchQueryMaxRunes — потолок длины query для /api/search (план §Р3).
// Анти-DoS + анти-prompt-stuffing: разумный поиск помещается в 2000 рун.
const SearchQueryMaxRunes = 2000

type searchRequestDTO struct {
	Query       string   `json:"query"`
	Limit       int      `json:"limit"`
	DocumentIDs []string `json:"document_ids,omitempty"`
}

type searchResultDTO struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	ChunkIndex int     `json:"chunk_index"`
	Snippet    string  `json:"snippet"` // sanitized, truncated до 512 рун
	Filename   string  `json:"filename"`
	Relevance  float64 `json:"relevance"`
	RiskLevel  *string `json:"risk_level,omitempty"`
}

type searchStatsDTO struct {
	QueryHadPII bool `json:"query_had_pii"`
	LatencyMs   int  `json:"latency_ms"`
}

type searchResponseDTO struct {
	Results []searchResultDTO `json:"results"`
	Stats   searchStatsDTO    `json:"stats"`
}

// searchHandler — POST /api/search. Pipeline (план §Р3):
//
//  1. rate-limit (30 RPM per user; 429 + Retry-After + audit one-per-window);
//  2. validate (query: 1..2000 рун; limit clamp 1..20);
//  3. sanitize query;
//  4. embed (через DI embedder, fail-closed на ошибке);
//  5. SearchChunks (ACL + embedder-name guard + documentIDs filter);
//  6. audit `search_performed` (расширенный detail);
//  7. audit `acl_violation_attempt` если в documentIDs были чужие ID
//     (диагностика BLOCKER B1 в проде).
//
// Ответ — JSON `{results, stats{query_had_pii, latency_ms}}`.
func searchHandler(
	store *storage.Storage, sanClient *sanitizer.Client,
	embedder llm.Embedder, limiter *UserRateLimiter,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()

		var dto searchRequestDTO
		if err := decodeJSON(w, r, &dto); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if dto.Query == "" {
			http.Error(w, "query обязателен", http.StatusBadRequest)
			return
		}
		if runeCount(dto.Query) > SearchQueryMaxRunes {
			http.Error(w, fmt.Sprintf("query > %d символов",
				SearchQueryMaxRunes), http.StatusBadRequest)
			return
		}

		userID, _ := currentUserID(r, store)
		role, _ := auth.RoleFromContext(r.Context())

		// Rate-limit per user (план §Р6). Один audit за окно.
		if limiter != nil {
			ok, shouldAudit := limiter.Allow(userID)
			if !ok {
				w.Header().Set("Retry-After",
					fmt.Sprintf("%d", limiter.RetryAfterSeconds()))
				if shouldAudit {
					_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
						UserID: userID, EventType: "rate_limit_exceeded",
						Detail: map[string]any{
							"endpoint":     "/api/search",
							"rpm_per_user": 30,
						},
					})
				}
				http.Error(w, "rate limit exceeded",
					http.StatusTooManyRequests)
				return
			}
		}

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

		// Embed sanitized query (план §Р2 fail-closed).
		queryVec, err := embedder.Embed(r.Context(), sanitizedQuery)
		if err != nil {
			http.Error(w, "ошибка embedder",
				http.StatusInternalServerError)
			return
		}

		// Поиск с ACL + embedder-name guard + documentIDs (§Р3, BLOCKER B1).
		results, err := store.SearchChunks(r.Context(), queryVec,
			userID, string(role), embedder.Name(),
			dto.DocumentIDs, dto.Limit)
		if err != nil {
			http.Error(w, "ошибка поиска",
				http.StatusInternalServerError)
			return
		}

		latencyMs := int(time.Since(started).Milliseconds())
		writeSearchAudit(r, store, userID, dto, results, hasPII,
			embedder.Name(), latencyMs)

		// Audit acl_violation_attempt: фиксируем только если часть
		// запрошенных document_ids НЕ ПРОШЛА ACL (то есть фактически
		// недоступна user'у). НЕ путать с limit-truncation: документ
		// доступен, но просто не попал в top-K по relevance.
		// Сравниваем `len(allowedByACL) vs len(requested)` через
		// отдельный SQL `FilterAccessibleDocuments` — иначе false-positive
		// при limit clamping (диагностика BLOCKER B1, плана §Р3).
		if len(dto.DocumentIDs) > 0 {
			allowed, _ := store.FilterAccessibleDocuments(
				r.Context(), userID, string(role), dto.DocumentIDs)
			if len(allowed) < len(dto.DocumentIDs) {
				_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
					UserID: userID, EventType: "acl_violation_attempt",
					Detail: map[string]any{
						"endpoint":      "/api/search",
						"requested":     len(dto.DocumentIDs),
						"allowed":       len(allowed),
						"blocked_count": len(dto.DocumentIDs) - len(allowed),
					},
				})
			}
		}

		out := make([]searchResultDTO, 0, len(results))
		for _, r := range results {
			out = append(out, searchResultDTO{
				ChunkID: r.ChunkID, DocumentID: r.DocumentID,
				ChunkIndex: r.ChunkIndex, Snippet: r.Snippet,
				Filename: r.Filename, Relevance: r.Relevance,
				RiskLevel: r.RiskLevel,
			})
		}
		writeJSON(w, http.StatusOK, searchResponseDTO{
			Results: out,
			Stats: searchStatsDTO{
				QueryHadPII: hasPII, LatencyMs: latencyMs,
			},
		})
	}
}

// writeSearchAudit пишет расширенный audit `search_performed` (§Р5):
// query_hash[:16], top_document_ids, top_chunk_ids, scores_summary,
// rag_mode='explicit', latency_ms, embedder_model. Plaintext query НЕ
// сохраняется нигде.
func writeSearchAudit(
	r *http.Request, store *storage.Storage, userID string,
	dto searchRequestDTO, results []storage.SearchResult,
	hasPII bool, embedderModel string, latencyMs int,
) {
	queryHash := sha256.Sum256([]byte(dto.Query))
	topDocs := make([]string, 0, len(results))
	topChunks := make([]string, 0, len(results))
	var maxScore, minScore float64
	for i, r := range results {
		topDocs = append(topDocs, r.DocumentID)
		topChunks = append(topChunks, r.ChunkID)
		if i == 0 {
			maxScore, minScore = r.Relevance, r.Relevance
		} else {
			if r.Relevance > maxScore {
				maxScore = r.Relevance
			}
			if r.Relevance < minScore {
				minScore = r.Relevance
			}
		}
	}
	detail := map[string]any{
		"query_hash":        hex.EncodeToString(queryHash[:8]),
		"query_len":         runeCount(dto.Query),
		"result_count":      len(results),
		"top_document_ids":  topDocs,
		"top_chunk_ids":     topChunks,
		"has_sanitized_pii": hasPII,
		"rag_mode":          "explicit",
		"latency_ms":        latencyMs,
		"embedder_model":    embedderModel,
	}
	if len(results) > 0 {
		detail["scores_summary"] = map[string]float64{
			"max": maxScore, "min": minScore,
		}
	}
	_, _ = store.InsertAuditEvent(r.Context(), storage.AuditEvent{
		UserID: userID, EventType: "search_performed",
		Detail: detail,
	})
}

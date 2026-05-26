package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// ragBudgetRunes — суммарный бюджет рун, отводимых RAG-чанкам в LLM-context'е
// (план §Р4: «Лимит контекста ≤ 5120 токенов» ≈ 5×1024 проксируется через
// 5×4096 рун для русского, ratio ≈ 4 рун/токен).
const ragBudgetRunes = 5 * 4096

// runRetrieval выполняет RAG-конвейер между Meta и runLLM (план §Р4):
// 1) Retrieve через DI-Retriever; 2) FilterHighRiskForExternal (audit на
// каждый dropped); 3) DetectSuspiciousPattern (audit, чанк всё равно
// идёт — false-positive безопасен); 4) TruncateByBudget; 5) sink.RagHits;
// 6) пересчёт политики с severity cap (+1 ступень); 7) audit rag_query.
//
// Возвращает (ragSystemPrompt, kept hits, revisedOutcome, ragWasUsed):
//   - ragSystemPrompt — текст system-prefix для LLM; "" если hits нет;
//   - revisedOutcome  — новое решение политики (== orig, если не повышалось);
//   - ragWasUsed      — true если ретривер вызывался (для метрик/аудита).
//
// Ошибки Retriever логируются как warning и graceful degradation:
// runLLM вызывается без RAG. Это сознательное решение (план §Р4):
// RAG — best-effort обогащение, его сбой не должен срывать чат.
func (o *Orchestrator) runRetrieval(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome,
) (ragSystem string, kept []RAGHit, revised policy.Outcome, used bool) {
	revised = outcome
	if o.retriever == nil || req.RAG == nil || !req.RAG.Enabled {
		return "", nil, outcome, false
	}
	// При deny/escalate RAG бессмыслен — LLM не вызовется, а retrieval ещё и
	// логически опасен (audit/трафик без последствий). План §Р4 D2.
	if outcome.Decision == policy.DecisionDeny ||
		outcome.Decision == policy.DecisionEscalate {
		return "", nil, outcome, false
	}

	started := time.Now()
	used = true
	hits, err := o.retriever.Retrieve(ctx, preview.SanitizedText, *req.RAG,
		req.UserID, req.UserRole)
	if err != nil {
		slog.WarnContext(ctx, "rag retrieve failed (graceful)",
			"request_id", req.RequestID, "error", err)
		o.recordAuditEvent(ctx, storage.AuditEvent{
			UserID: req.UserID, EventType: "rag_query",
			Detail: map[string]any{
				"request_id": req.RequestID,
				"rag_mode":   "chat_integrated",
				"error":      true,
				"latency_ms": int(time.Since(started).Milliseconds()),
			},
		})
		return "", nil, outcome, true
	}

	// Step 1: пересчёт политики ПО ВСЕМ retrieved hits (включая те, что
	// будут дропнуты для external — сам факт ACL-доступа к чувствительному
	// документу повышает severity). План §Р4 D2.
	revised = o.maybeRevisePolicy(ctx, req, preview, outcome, hits)
	// Если после пересчёта решение блокирующее — RAG-инъекция бессмысленна
	// (LLM не вызовется) и опасна (rag_hits перечисляет документы из ACL —
	// утечка списка КЛИЕНТУ). Но в audit hits сохраняем: ИБ-офицеру нужны
	// top_document_ids для расследования «почему именно этот запрос
	// заблокирован», и сервер-side journal — не клиент. План §Р4 D2 +
	// ревью архитектора Итерации 11 MINOR-1.
	if revised.Decision == policy.DecisionDeny ||
		revised.Decision == policy.DecisionEscalate {
		o.writeRagQueryAudit(ctx, req, preview, hits, started)
		return "", nil, revised, true
	}

	// Step 2: external-LLM не должен получать high/critical чанки даже
	// после masking (псевдонимы могут косвенно раскрывать). План §Р4 MINOR m4.
	isExternal := req.ModelTrust == string(policy.TrustExternal) ||
		req.ModelTrust == string(policy.TrustRussianCloud)
	kept, dropped := FilterHighRiskForExternal(hits, isExternal)
	for _, d := range dropped {
		level := ""
		if d.RiskLevel != nil {
			level = *d.RiskLevel
		}
		o.recordAuditEvent(ctx, storage.AuditEvent{
			UserID: req.UserID, EventType: "rag_chunk_dropped_high_risk",
			Detail: map[string]any{
				"request_id":  req.RequestID,
				"document_id": d.DocumentID,
				"chunk_index": d.ChunkIndex,
				"risk_level":  level,
				"trust_level": req.ModelTrust,
			},
		})
	}

	// Step 3: suspicious-pattern detection на каждом оставшемся.
	// false-positive безопасен — чанк всё равно идёт, но событие фиксируется
	// (security-officer расследует). План §Р4 MAJOR M1.
	for _, h := range kept {
		if DetectSuspiciousPattern(h.Snippet) {
			o.recordAuditEvent(ctx, storage.AuditEvent{
				UserID: req.UserID, EventType: "rag_chunk_suspicious_pattern",
				Detail: map[string]any{
					"request_id":  req.RequestID,
					"document_id": h.DocumentID,
					"chunk_index": h.ChunkIndex,
				},
			})
		}
	}

	// Step 4: TopK + token-budget truncation.
	kept = TruncateByBudget(kept, ragBudgetRunes)

	// Step 5/7: формируем system-prompt и пишем agreg-audit rag_query.
	ragSystem = BuildRAGSystemPrompt(kept)
	o.writeRagQueryAudit(ctx, req, preview, kept, started)

	return ragSystem, kept, revised, true
}

// --- severity cap + пересчёт политики (план §Р4 D2) ---

// riskOrder — числовой порядок risk_level. Возвращает -1 для пустых/неизвестных.
func riskOrder(level string) int {
	switch level {
	case "low":
		return 0
	case "medium":
		return 1
	case "high":
		return 2
	case "critical":
		return 3
	}
	return -1
}

// decisionOrder — severity-порядок решений политики
// (allow_raw < allow_masked < allow_summary_only < escalate < deny).
// План §Р4 D2: cap повышения после RAG = max +1 ступень.
func decisionOrder(d policy.Decision) int {
	switch d {
	case policy.DecisionAllowRaw:
		return 0
	case policy.DecisionAllowMasked:
		return 1
	case policy.DecisionAllowSummaryOnly:
		return 2
	case policy.DecisionEscalate:
		return 3
	case policy.DecisionDeny:
		return 4
	}
	return 0
}

// decisionByOrder — обратное отображение порядок → Decision.
func decisionByOrder(o int) policy.Decision {
	switch o {
	case 0:
		return policy.DecisionAllowRaw
	case 1:
		return policy.DecisionAllowMasked
	case 2:
		return policy.DecisionAllowSummaryOnly
	case 3:
		return policy.DecisionEscalate
	default:
		return policy.DecisionDeny
	}
}

// maxHitRiskLevel находит наибольший risk_level среди hits (nil → -1).
func maxHitRiskLevel(hits []RAGHit) string {
	maxRank := -1
	maxLvl := ""
	for _, h := range hits {
		if h.RiskLevel == nil {
			continue
		}
		r := riskOrder(*h.RiskLevel)
		if r > maxRank {
			maxRank = r
			maxLvl = *h.RiskLevel
		}
	}
	return maxLvl
}

// maybeRevisePolicy пересчитывает решение с учётом риска retrieved-чанков
// (план §Р4 D2). Cap: повышение НЕ более чем на одну ступень в шкале
// allow_raw < allow_masked < allow_summary_only < escalate < deny.
// Этот cap критичен: иначе один critical-чанк в ACL пользователя
// гарантирует escalate любого его запроса (DoS-вектор).
//
// Триггер: max risk_level среди hits строго выше preview.Risk.Level.
// Re-running policy.Decide здесь намеренно НЕ используется (классы
// чанков недоступны в storage.SearchResult — только level). Cap-based
// upgrade даёт предсказуемое поведение независимо от классов hits.
func (o *Orchestrator) maybeRevisePolicy(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	orig policy.Outcome, hits []RAGHit,
) policy.Outcome {
	if len(hits) == 0 {
		return orig
	}
	maxLvl := maxHitRiskLevel(hits)
	origLvl := preview.Risk.Level
	if maxLvl == "" || riskOrder(maxLvl) <= riskOrder(origLvl) {
		return orig
	}

	// Cap: +1 ступень от orig.Decision (не выше deny).
	capRank := decisionOrder(orig.Decision) + 1
	if capRank > 4 {
		capRank = 4
	}
	revised := orig
	revised.Decision = decisionByOrder(capRank)
	// elevated_risk_level — proxy-уровень для audit: тоже не выше +1 ступени.
	elevatedRank := riskOrder(origLvl) + 1
	if elevatedRank > riskOrder(maxLvl) {
		elevatedRank = riskOrder(maxLvl)
	}
	if elevatedRank > 3 {
		elevatedRank = 3
	}
	elevatedLevel := []string{"low", "medium", "high", "critical"}[elevatedRank]

	// Audit policy_revised_after_rag (rate-limit 10/час; throttled один раз на окно).
	allowed, throttled := o.ragPolicyRevisedReporter.Allow(req.UserID)
	if allowed {
		o.recordAuditEvent(ctx, storage.AuditEvent{
			UserID: req.UserID, EventType: "policy_revised_after_rag",
			Detail: map[string]any{
				"request_id":          req.RequestID,
				"original_decision":   string(orig.Decision),
				"revised_decision":    string(revised.Decision),
				"original_risk_level": origLvl,
				"elevated_risk_level": elevatedLevel,
				"rag_max_risk_level":  maxLvl,
			},
		})
	} else if throttled {
		o.recordAuditEvent(ctx, storage.AuditEvent{
			UserID: req.UserID, EventType: "policy_revised_after_rag_throttled",
			Detail: map[string]any{
				"request_id":   req.RequestID,
				"window_limit": 10,
			},
		})
	}

	revised.Reasons = append([]string{
		fmt.Sprintf("RAG-источник риска уровня %s повысил решение", maxLvl),
	}, revised.Reasons...)
	return revised
}

// writeRagQueryAudit — единый agreg-audit на стрим (план §Р4 Шаг 10).
func (o *Orchestrator) writeRagQueryAudit(
	ctx context.Context, req Request, preview sanitizer.PreviewResponse,
	hits []RAGHit, started time.Time,
) {
	queryHash := sha256.Sum256([]byte(preview.SanitizedText))
	topDocs := make([]string, 0, len(hits))
	topChunks := make([]int, 0, len(hits))
	for _, h := range hits {
		topDocs = append(topDocs, h.DocumentID)
		topChunks = append(topChunks, h.ChunkIndex)
	}
	o.recordAuditEvent(ctx, storage.AuditEvent{
		UserID: req.UserID, EventType: "rag_query",
		Detail: map[string]any{
			"request_id":       req.RequestID,
			"rag_mode":         "chat_integrated",
			"query_hash":       hex.EncodeToString(queryHash[:8]),
			"top_document_ids": topDocs,
			"top_chunk_idx":    topChunks,
			"result_count":     len(hits),
			"latency_ms":       int(time.Since(started).Milliseconds()),
		},
	})
}

// applyRagToMessages добавляет system-prefix с RAG-контекстом к user-сообщению.
// Сохраняет уже существующий system (для summary-mode), просто кладёт RAG
// system ПОСЛЕ оригинального — модель видит сначала «отвечай кратко…»,
// затем «вот данные…», затем user.
func applyRagToMessages(msgs []llm.ChatMessage, ragSystem string) []llm.ChatMessage {
	if ragSystem == "" {
		return msgs
	}
	out := make([]llm.ChatMessage, 0, len(msgs)+1)
	// Сначала любые имеющиеся system-сообщения (summary, безопасность и т. п.)
	i := 0
	for i < len(msgs) && msgs[i].Role == "system" {
		out = append(out, msgs[i])
		i++
	}
	out = append(out, llm.ChatMessage{Role: "system", Content: ragSystem})
	out = append(out, msgs[i:]...)
	return out
}

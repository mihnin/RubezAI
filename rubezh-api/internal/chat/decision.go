package chat

import "github.com/rubezh-ai/rubezh-api/internal/policy"

// action — что оркестратору делать с решением политики.
type action struct {
	callLLM     bool
	sendText    string // текст для отправки в LLM
	restore     bool   // восстанавливать псевдонимы в ответе
	summaryMode bool   // summary: restore не делать, утечку ре-маскировать
}

// actionFor отображает решение политики в действие оркестратора (план Р3).
func actionFor(
	decision policy.Decision, originalText, sanitizedText string,
) action {
	switch decision {
	case policy.DecisionAllowRaw:
		return action{callLLM: true, sendText: originalText}
	case policy.DecisionAllowMasked:
		return action{callLLM: true, sendText: sanitizedText, restore: true}
	case policy.DecisionAllowSummaryOnly:
		return action{callLLM: true, sendText: sanitizedText, summaryMode: true}
	default: // deny, escalate
		return action{callLLM: false}
	}
}

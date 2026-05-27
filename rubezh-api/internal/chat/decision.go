package chat

import (
	"strings"

	"github.com/rubezh-ai/rubezh-api/internal/llm"
	"github.com/rubezh-ai/rubezh-api/internal/policy"
)

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

// buildLLMMessages формирует список сообщений для LLM. Для summary-режима
// предваряет user-сообщение system-инструкцией; гарантию безопасности даёт
// отсутствие restore (план Р3, MAJOR-3).
func buildLLMMessages(act action, systemPrompt string) []llm.ChatMessage {
	var msgs []llm.ChatMessage
	if p := strings.TrimSpace(systemPrompt); p != "" {
		msgs = append(msgs, llm.ChatMessage{Role: "system", Content: p})
	}
	if act.summaryMode {
		msgs = append(msgs,
			llm.ChatMessage{Role: "system", Content: "Ответь кратким резюме, не повторяя детали."},
		)
	}
	msgs = append(msgs, llm.ChatMessage{Role: "user", Content: act.sendText})
	return msgs
}

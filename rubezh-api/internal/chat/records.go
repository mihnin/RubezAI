package chat

import (
	"encoding/json"
	"strings"

	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// requestRecord строит запись Транзакции 1 (chat_request).
func (o *Orchestrator) requestRecord(
	req Request, preview sanitizer.PreviewResponse, outcome policy.Outcome,
) storage.ChatRequestRecord {
	return storage.ChatRequestRecord{
		SessionID:   req.SessionID,
		UserContent: preview.SanitizedText,
		Sanitization: storage.SanitizationData{
			RiskLevel:   preview.Risk.Level,
			RiskScore:   preview.Risk.Score,
			RiskClasses: preview.Risk.Classes,
			Entities:    entitiesJSON(preview.Entities),
		},
		Audit: o.auditEvent(req, preview, outcome, "chat_request",
			map[string]any{
				"request_id":   req.RequestID,
				"entity_count": len(preview.Entities),
			}),
	}
}

// terminationRecord строит запись Транзакции 2 (chat_response/chat_blocked).
func (o *Orchestrator) terminationRecord(
	req Request, preview sanitizer.PreviewResponse, outcome policy.Outcome,
	eventType, assistantContent string, leaked []string,
) storage.ChatTerminationRecord {
	detail := map[string]any{"request_id": req.RequestID}
	if len(leaked) > 0 {
		detail["response_leak_detected"] = true
		detail["leaked_pseudonyms"] = leaked
	}
	providerID := req.ProviderID
	return storage.ChatTerminationRecord{
		SessionID:        req.SessionID,
		AssistantContent: assistantContent,
		ModelProviderID:  &providerID,
		Audit:            o.auditEvent(req, preview, outcome, eventType, detail),
	}
}

// auditEvent собирает запись аудита из общих полей запроса и решения.
func (o *Orchestrator) auditEvent(
	req Request, preview sanitizer.PreviewResponse, outcome policy.Outcome,
	eventType string, detail map[string]any,
) storage.AuditEvent {
	level := preview.Risk.Level
	decision := string(outcome.Decision)
	payload := preview.SanitizedText
	providerID := req.ProviderID
	return storage.AuditEvent{
		UserID:          req.UserID,
		EventType:       eventType,
		ModelProviderID: &providerID,
		RiskLevel:       &level,
		RiskClasses:     preview.Risk.Classes,
		PolicyDecision:  &decision,
		MatchedRule:     outcome.MatchedRule,
		MaskedPayload:   &payload,
		Detail:          detail,
	}
}

// errorEvent строит минимальную запись chat_error (до решения политики).
func (o *Orchestrator) errorEvent(
	req Request, detail map[string]any,
) storage.AuditEvent {
	if detail == nil {
		detail = map[string]any{}
	}
	detail["request_id"] = req.RequestID
	ev := storage.AuditEvent{
		UserID:    req.UserID,
		EventType: "chat_error",
		Detail:    detail,
	}
	if req.ProviderID != "" {
		providerID := req.ProviderID
		ev.ModelProviderID = &providerID
	}
	return ev
}

// policyErrorEvent — chat_error после принятия решения политики:
// дополняет errorEvent риском и решением.
func (o *Orchestrator) policyErrorEvent(
	req Request, preview sanitizer.PreviewResponse,
	outcome policy.Outcome, detail map[string]any,
) storage.AuditEvent {
	ev := o.errorEvent(req, detail)
	level := preview.Risk.Level
	decision := string(outcome.Decision)
	payload := preview.SanitizedText
	ev.RiskLevel = &level
	ev.RiskClasses = preview.Risk.Classes
	ev.PolicyDecision = &decision
	ev.MatchedRule = outcome.MatchedRule
	ev.MaskedPayload = &payload
	return ev
}

// sanitizedErrorEvent — chat_error после успешного sanitize, до принятия
// решения политики: включает риск из preview, но не decision/matched_rule.
// Используется при сбое построения карты псевдонимов.
func (o *Orchestrator) sanitizedErrorEvent(
	req Request, preview sanitizer.PreviewResponse, detail map[string]any,
) storage.AuditEvent {
	ev := o.errorEvent(req, detail)
	level := preview.Risk.Level
	payload := preview.SanitizedText
	ev.RiskLevel = &level
	ev.RiskClasses = preview.Risk.Classes
	ev.MaskedPayload = &payload
	return ev
}

// metaFor строит MetaEvent; для summary добавляет поясняющую причину.
func metaFor(
	req Request, preview sanitizer.PreviewResponse, outcome policy.Outcome,
) MetaEvent {
	reasons := append([]string(nil), outcome.Reasons...)
	if outcome.Decision == policy.DecisionAllowSummaryOnly {
		reasons = append(reasons,
			"Ответ обезличен: показаны псевдонимы (режим резюме)")
	}
	return MetaEvent{
		Decision: string(outcome.Decision),
		Risk: RiskView{
			Level:   preview.Risk.Level,
			Score:   preview.Risk.Score,
			Classes: preview.Risk.Classes,
		},
		Provider:  req.Provider,
		Reasons:   reasons,
		RequestID: req.RequestID,
	}
}

// finalTexts вычисляет (сохраняемый, стримируемый) варианты ответа.
func finalTexts(
	act action, pmap PseudonymMap, rawOutput string,
) (stored, streamed string) {
	switch {
	case act.summaryMode:
		remasked := pmap.Remask(rawOutput)
		return remasked, remasked
	case act.restore:
		return rawOutput, pmap.Restore(rawOutput)
	default: // allow_raw
		return rawOutput, rawOutput
	}
}

// blockedNotice формирует уведомление пользователю о блокировке.
func blockedNotice(outcome policy.Outcome) string {
	reason := strings.Join(outcome.Reasons, "; ")
	prefix := "Запрос отклонён политикой безопасности."
	if outcome.Decision == policy.DecisionEscalate {
		prefix = "Запрос направлен на согласование специалисту ИБ."
	}
	return strings.TrimSpace(prefix + " " + reason)
}

// chunkText режет текст на чанки по size рун (псевдо-стриминг SSE).
func chunkText(s string, size int) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var chunks []string
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

// entitiesJSON сериализует сущности в JSON-массив (пустой → []).
func entitiesJSON(entities []sanitizer.Entity) json.RawMessage {
	if len(entities) == 0 {
		return json.RawMessage("[]")
	}
	data, err := json.Marshal(entities)
	if err != nil {
		return json.RawMessage("[]")
	}
	return data
}

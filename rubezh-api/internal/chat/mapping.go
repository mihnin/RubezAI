package chat

import (
	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/sanitizer"
)

// ToPolicyInput строит вход policy engine из ответа sanitizer.
// Типы сущностей дедуплицируются; context фиксирован как "chat".
func ToPolicyInput(
	resp sanitizer.PreviewResponse, modelTrust, userRole string,
) policy.Input {
	classes := make([]policy.RiskClass, len(resp.Risk.Classes))
	for i, c := range resp.Risk.Classes {
		classes[i] = policy.RiskClass(c)
	}
	seen := make(map[string]bool, len(resp.Entities))
	types := make([]string, 0, len(resp.Entities))
	for _, e := range resp.Entities {
		if !seen[e.Type] {
			seen[e.Type] = true
			types = append(types, e.Type)
		}
	}
	return policy.Input{
		ModelTrust: policy.ModelTrust(modelTrust),
		Risk: policy.Risk{
			Level:   policy.RiskLevel(resp.Risk.Level),
			Classes: classes,
			Score:   resp.Risk.Score,
		},
		EntityTypes: types,
		UserRole:    userRole,
		Context:     "chat",
	}
}

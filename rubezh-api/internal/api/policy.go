package api

import (
	"encoding/json"
	"net/http"

	"github.com/rubezh-ai/rubezh-api/internal/policy"
	"github.com/rubezh-ai/rubezh-api/internal/storage"
)

// DTO HTTP-слоя политик, согласованные с docs/contracts/policy.schema.json.

type riskDTO struct {
	Level   string   `json:"level"`
	Classes []string `json:"classes"`
	Score   float64  `json:"score"`
}

type policyInputDTO struct {
	ModelTrust  string   `json:"model_trust"`
	Risk        riskDTO  `json:"risk"`
	EntityTypes []string `json:"entity_types"`
	UserRole    string   `json:"user_role"`
	Context     string   `json:"context"`
}

type policyDecisionDTO struct {
	Decision             string   `json:"decision"`
	MatchedPolicyID      *string  `json:"matched_policy_id"`
	MatchedPolicyVersion *int     `json:"matched_policy_version"`
	MatchedRule          *string  `json:"matched_rule"`
	Reasons              []string `json:"reasons"`
}

type policyDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	IsActive       bool   `json:"is_active"`
	CurrentVersion int    `json:"current_version"`
}

type createPolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (d policyInputDTO) toInput() policy.Input {
	classes := make([]policy.RiskClass, len(d.Risk.Classes))
	for i, c := range d.Risk.Classes {
		classes[i] = policy.RiskClass(c)
	}
	return policy.Input{
		ModelTrust: policy.ModelTrust(d.ModelTrust),
		Risk: policy.Risk{
			Level:   policy.RiskLevel(d.Risk.Level),
			Classes: classes,
			Score:   d.Risk.Score,
		},
		EntityTypes: d.EntityTypes,
		UserRole:    d.UserRole,
		Context:     d.Context,
	}
}

func decisionToDTO(o policy.Outcome) policyDecisionDTO {
	return policyDecisionDTO{
		Decision:             string(o.Decision),
		MatchedPolicyID:      o.MatchedPolicyID,
		MatchedPolicyVersion: o.MatchedPolicyVersion,
		MatchedRule:          o.MatchedRule,
		Reasons:              o.Reasons,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// policyTestHandler прогоняет встроенную политику на присланном PolicyInput
// (эндпойнт «тест политики на примере запроса»).
func policyTestHandler(w http.ResponseWriter, r *http.Request) {
	var input policyInputDTO
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "некорректный JSON", http.StatusBadRequest)
		return
	}
	outcome := policy.DefaultPolicy().Decide(input.toInput())
	writeJSON(w, http.StatusOK, decisionToDTO(outcome))
}

// listPoliciesHandler возвращает список сохранённых политик.
func listPoliciesHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policies, err := store.ListPolicies(r.Context())
		if err != nil {
			http.Error(w, "ошибка чтения политик", http.StatusInternalServerError)
			return
		}
		out := make([]policyDTO, len(policies))
		for i, p := range policies {
			out[i] = policyDTO(p)
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// createPolicyHandler создаёт новую политику с первой версией.
func createPolicyHandler(store *storage.Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createPolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "некорректный JSON", http.StatusBadRequest)
			return
		}
		if req.Name == "" {
			http.Error(w, "поле name обязательно", http.StatusBadRequest)
			return
		}
		created, err := store.CreatePolicy(r.Context(), req.Name, req.Description)
		if err != nil {
			http.Error(w, "не удалось создать политику", http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusCreated, policyDTO(created))
	}
}

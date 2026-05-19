// Package policy — детерминированный движок политик «Рубеж ИИ».
//
// Финальное решение allow/deny принимает только этот пакет (принцип
// policy-decided). Вход и решение согласованы с docs/contracts/policy.schema.json.
package policy

// ModelTrust — режим доверия модели.
type ModelTrust string

// Режимы доверия модели.
const (
	TrustExternal     ModelTrust = "external"
	TrustRussianCloud ModelTrust = "russian_cloud"
	TrustOnPrem       ModelTrust = "on_prem"
	TrustTrustedLocal ModelTrust = "trusted_local"
)

// RiskLevel — уровень риска.
type RiskLevel string

// Уровни риска.
const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// RiskClass — класс чувствительных данных.
type RiskClass string

// Классы чувствительных данных.
const (
	ClassPII        RiskClass = "pii"
	ClassSecret     RiskClass = "secret"
	ClassCommercial RiskClass = "commercial"
)

// Decision — решение политики.
type Decision string

// Возможные решения политики.
const (
	DecisionAllowRaw         Decision = "allow_raw"
	DecisionAllowMasked      Decision = "allow_masked"
	DecisionAllowSummaryOnly Decision = "allow_summary_only"
	DecisionDeny             Decision = "deny"
	DecisionEscalate         Decision = "escalate"
)

// Risk — агрегированная оценка риска запроса.
type Risk struct {
	Level   RiskLevel
	Classes []RiskClass
	Score   float64
}

// HasClass сообщает, присутствует ли класс риска.
func (r Risk) HasClass(class RiskClass) bool {
	for _, c := range r.Classes {
		if c == class {
			return true
		}
	}
	return false
}

// Input — вход движка политик (контракт PolicyInput).
type Input struct {
	ModelTrust  ModelTrust
	Risk        Risk
	EntityTypes []string
	UserRole    string
	Context     string
}

// Outcome — решение движка политик (контракт PolicyDecision).
type Outcome struct {
	Decision             Decision
	MatchedPolicyID      *string
	MatchedPolicyVersion *int
	MatchedRule          *string
	Reasons              []string
}

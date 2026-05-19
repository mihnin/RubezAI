package policy

// Rule — одно правило политики: условие и решение при срабатывании.
type Rule struct {
	Name     string
	Decision Decision
	Reason   string
	Match    func(Input) bool
}

// Policy — упорядоченный набор правил. Решает первое сработавшее правило.
type Policy struct {
	Name  string
	Rules []Rule
}

// Decide применяет правила по порядку и возвращает решение первого
// сработавшего. Если не сработало ни одно — безопасный по умолчанию deny
// (fail-closed).
func (p Policy) Decide(in Input) Outcome {
	for _, rule := range p.Rules {
		if rule.Match(in) {
			name := rule.Name
			return Outcome{
				Decision:    rule.Decision,
				MatchedRule: &name,
				Reasons:     []string{rule.Reason},
			}
		}
	}
	return Outcome{
		Decision: DecisionDeny,
		Reasons:  []string{"ни одно правило политики не разрешило запрос"},
	}
}

func isSensitive(in Input) bool {
	return in.Risk.HasClass(ClassPII) || in.Risk.HasClass(ClassCommercial)
}

func isExternalTrust(trust ModelTrust) bool {
	return trust == TrustExternal || trust == TrustRussianCloud
}

// DefaultPolicy возвращает встроенную политику MVP (принцип «rules-first»).
// Правила упорядочены от самых строгих к разрешающим; первое сработавшее
// определяет решение.
func DefaultPolicy() Policy {
	return Policy{
		Name: "default",
		Rules: []Rule{
			{
				Name:     "secret-deny",
				Decision: DecisionDeny,
				Reason:   "обнаружены секреты — отправка в любую LLM запрещена",
				Match:    func(in Input) bool { return in.Risk.HasClass(ClassSecret) },
			},
			{
				Name:     "critical-escalate",
				Decision: DecisionEscalate,
				Reason:   "критический уровень риска — требуется решение службы ИБ",
				Match:    func(in Input) bool { return in.Risk.Level == RiskCritical },
			},
			{
				Name:     "external-sensitive-masked",
				Decision: DecisionAllowMasked,
				Reason:   "внешняя модель получает только обезличенный текст",
				Match: func(in Input) bool {
					return isExternalTrust(in.ModelTrust) && isSensitive(in)
				},
			},
			{
				Name:     "external-clean-raw",
				Decision: DecisionAllowRaw,
				Reason:   "чувствительные данные не обнаружены",
				Match:    func(in Input) bool { return isExternalTrust(in.ModelTrust) },
			},
			{
				Name:     "onprem-sensitive-masked",
				Decision: DecisionAllowMasked,
				Reason:   "on-prem модель получает обезличенный текст",
				Match: func(in Input) bool {
					return in.ModelTrust == TrustOnPrem && isSensitive(in)
				},
			},
			{
				Name:     "trusted-raw",
				Decision: DecisionAllowRaw,
				Reason:   "доверенная модель в периметре заказчика",
				Match: func(in Input) bool {
					return in.ModelTrust == TrustOnPrem || in.ModelTrust == TrustTrustedLocal
				},
			},
		},
	}
}

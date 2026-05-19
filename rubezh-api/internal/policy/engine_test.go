package policy

import "testing"

func TestDefaultPolicyDecisionTable(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  Decision
	}{
		{
			"секрет — запрет даже на доверенной модели",
			Input{ModelTrust: TrustTrustedLocal, Risk: Risk{
				Level: RiskHigh, Classes: []RiskClass{ClassSecret}}},
			DecisionDeny,
		},
		{
			"критический риск — эскалация в ИБ",
			Input{ModelTrust: TrustOnPrem, Risk: Risk{
				Level: RiskCritical, Classes: []RiskClass{ClassPII}}},
			DecisionEscalate,
		},
		{
			"внешняя модель + ПДн — обезличивание",
			Input{ModelTrust: TrustExternal, Risk: Risk{
				Level: RiskMedium, Classes: []RiskClass{ClassPII}}},
			DecisionAllowMasked,
		},
		{
			"российское облако + коммерческие данные — обезличивание",
			Input{ModelTrust: TrustRussianCloud, Risk: Risk{
				Level: RiskMedium, Classes: []RiskClass{ClassCommercial}}},
			DecisionAllowMasked,
		},
		{
			"внешняя модель, чувствительного нет — raw",
			Input{ModelTrust: TrustExternal, Risk: Risk{Level: RiskLow}},
			DecisionAllowRaw,
		},
		{
			"on-prem + ПДн — обезличивание",
			Input{ModelTrust: TrustOnPrem, Risk: Risk{
				Level: RiskMedium, Classes: []RiskClass{ClassPII}}},
			DecisionAllowMasked,
		},
		{
			"доверенная локальная + ПДн — raw",
			Input{ModelTrust: TrustTrustedLocal, Risk: Risk{
				Level: RiskMedium, Classes: []RiskClass{ClassPII}}},
			DecisionAllowRaw,
		},
		{
			"on-prem, чувствительного нет — raw",
			Input{ModelTrust: TrustOnPrem, Risk: Risk{Level: RiskLow}},
			DecisionAllowRaw,
		},
	}
	pol := DefaultPolicy()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pol.Decide(tc.input)
			if got.Decision != tc.want {
				t.Errorf("Decide = %q, ожидалось %q", got.Decision, tc.want)
			}
			if len(got.Reasons) == 0 {
				t.Error("решение без причин (нужны для audit)")
			}
			if got.MatchedRule == nil || *got.MatchedRule == "" {
				t.Error("не указано сработавшее правило")
			}
		})
	}
}

func TestSecretDenyPrecedesCriticalEscalate(t *testing.T) {
	// секрет + критический риск одновременно → deny (правило секретов раньше)
	out := DefaultPolicy().Decide(Input{
		ModelTrust: TrustTrustedLocal,
		Risk: Risk{
			Level:   RiskCritical,
			Classes: []RiskClass{ClassSecret, ClassPII},
		},
	})
	if out.Decision != DecisionDeny {
		t.Errorf("Decide = %q, ожидалось deny", out.Decision)
	}
}

func TestExternalNeverGetsRawSensitiveData(t *testing.T) {
	// инвариант: внешняя модель никогда не получает raw при наличии ПДн
	for _, trust := range []ModelTrust{TrustExternal, TrustRussianCloud} {
		out := DefaultPolicy().Decide(Input{
			ModelTrust: trust,
			Risk:       Risk{Level: RiskMedium, Classes: []RiskClass{ClassPII}},
		})
		if out.Decision == DecisionAllowRaw {
			t.Errorf("trust=%q: внешняя модель получила allow_raw при ПДн", trust)
		}
	}
}

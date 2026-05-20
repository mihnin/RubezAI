package chat

import (
	"testing"
)

// TestAutoIncidentTrigger — отображение decision→trigger.
func TestAutoIncidentTrigger(t *testing.T) {
	cases := []struct {
		name     string
		decision string
		leak     bool
		want     triggerKind
	}{
		{"deny", "deny", false, triggerDeny},
		{"deny+leak (leak игнорируется при deny)", "deny", true, triggerDeny},
		{"escalate", "escalate", false, triggerEscalate},
		{"allow_masked + leak", "allow_masked", true, triggerLeak},
		{"allow_summary_only + leak", "allow_summary_only", true, triggerLeak},
		{"allow_masked без leak", "allow_masked", false, ""},
		{"allow_raw без leak", "allow_raw", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := autoIncidentTrigger(tc.decision, tc.leak)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSeverityFor — таблица (risk_level, trigger) → severity
// (план §Р4 + M5 ревью v2: leak повышает на 2 ступени для low/medium).
func TestSeverityFor(t *testing.T) {
	cases := []struct {
		risk    string
		trigger triggerKind
		want    string
	}{
		// deny / escalate: severity = risk
		{"low", triggerDeny, "low"},
		{"medium", triggerDeny, "medium"},
		{"high", triggerDeny, "high"},
		{"critical", triggerDeny, "critical"},
		{"low", triggerEscalate, "low"},
		{"medium", triggerEscalate, "medium"},
		{"high", triggerEscalate, "high"},
		{"critical", triggerEscalate, "critical"},
		// leak: low/medium → +2 ступени (high/critical)
		{"low", triggerLeak, "high"},
		{"medium", triggerLeak, "critical"},
		{"high", triggerLeak, "critical"},
		{"critical", triggerLeak, "critical"},
	}
	for _, tc := range cases {
		got := severityFor(tc.risk, tc.trigger)
		if got != tc.want {
			t.Errorf("severityFor(%q, %q) = %q, want %q",
				tc.risk, tc.trigger, got, tc.want)
		}
	}
}

// TestAutoIncidentTitleAndSummary — корректные строки для UI.
func TestAutoIncidentTitleAndSummary(t *testing.T) {
	title := autoIncidentTitle(triggerLeak, "high")
	if title == "" || !contains(title, "high") || !contains(title, "замаскированное") {
		t.Errorf("title для leak неинформативный: %q", title)
	}
	sum := autoIncidentSummary(triggerDeny, "critical", []string{"secret"})
	if !contains(sum, "deny") || !contains(sum, "critical") {
		t.Errorf("summary для deny неинформативный: %q", sum)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

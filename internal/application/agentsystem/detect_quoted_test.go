package agentsystem_test

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestCheckShQuotedCaseLabelsDispatchLikeUnquotedOnes(t *testing.T) {
	for name, c := range map[string]struct {
		script string
		named  []domain.GateKind
	}{
		"double quoted":       {"case \"$1\" in\n  \"build\") make ;;\nesac\n", []domain.GateKind{domain.GateBuild}},
		"single quoted":       {"case \"$1\" in\n  'lint') x ;;\nesac\n", []domain.GateKind{domain.GateLint}},
		"quoted alternatives": {"case \"$1\" in\n  \"build\"|\"test\") make ;;\n  'lint' | 'secret_scan') x ;;\nesac\n", []domain.GateKind{domain.GateBuild, domain.GateTest, domain.GateLint, domain.GateSecretScan}},
	} {
		t.Run(name, func(t *testing.T) {
			got := detect(t, map[string]string{"check.sh": c.script}, noGitleaks)
			for _, k := range c.named {
				wantGate(t, got, k, "./check.sh "+string(k), "detected from check.sh")
			}
		})
	}
}

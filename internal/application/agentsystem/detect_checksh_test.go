package agentsystem_test

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestCheckShFillsSecretScanItNamesAndGitleaksDoesNotOverwriteIt(t *testing.T) {
	files := map[string]string{"check.sh": "case \"$1\" in\n  secret_scan) ./scan ;;\nesac\n"}
	for _, look := range []func(string) (string, error){withGitleaks, noGitleaks} {
		got := detect(t, files, look)
		wantGate(t, got, domain.GateSecretScan, "./check.sh secret_scan", "detected from check.sh")
	}
}

func TestCheckShDispatchIsRecognisedInEveryCommonShape(t *testing.T) {
	for name, c := range map[string]struct {
		script string
		named  []domain.GateKind
	}{
		"single-bracket if": {"if [ \"$1\" = build ]; then make; fi\nif [ \"$1\" == lint ]; then x; fi\n", []domain.GateKind{domain.GateBuild, domain.GateLint}},
		"double-bracket if": {"[[ $1 == \"test\" ]] && run\n[[ \"${1}\" = 'lockfile' ]] && run\n", []domain.GateKind{domain.GateTest, domain.GateLockfile}},
		"alternatives":      {"case \"$1\" in\n  build|test) make ;;\n  lint | secret-scan) x ;;\nesac\n", []domain.GateKind{domain.GateBuild, domain.GateTest, domain.GateLint, domain.GateSecretScan}},
		"parenthesised":     {"case $1 in\n  (build) make ;;\nesac\n", []domain.GateKind{domain.GateBuild}},
	} {
		t.Run(name, func(t *testing.T) {
			got := detect(t, map[string]string{"check.sh": c.script}, noGitleaks)
			for _, k := range c.named {
				wantGate(t, got, k, "./check.sh "+string(k), "detected from check.sh")
			}
			if len(c.named) > 0 && got[domain.GateTest].Command == "./check.sh" {
				t.Error("a dispatching script was treated as a plain one")
			}
		})
	}
}

func TestCheckShMentioningAKindWordWithoutDispatchingIsAPlainScript(t *testing.T) {
	got := detect(t, map[string]string{"check.sh": "#!/bin/sh\n# build and lint\nmake build lint\n"}, noGitleaks)
	wantGate(t, got, domain.GateTest, "./check.sh", "detected from check.sh")
	wantGate(t, got, domain.GateBuild, "", noDetection)
}

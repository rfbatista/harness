package domain

import (
	"testing"
	"time"
)

func TestAGateRunIsGreenOnlyWhenEveryGatePassed(t *testing.T) {
	ok := GateResult{Kind: GateBuild, ExitCode: 0}
	cases := []struct {
		name    string
		trigger GateTrigger
		gates   []GateResult
		missing []GateKind
		result  GateRunResult
		counted bool
	}{
		{"all exit 0", GateTriggerDeclaration, []GateResult{ok, ok}, nil, GateRunGreen, true},
		{"one non-zero", GateTriggerDeclaration, []GateResult{ok, {Kind: GateTest, ExitCode: 1}}, nil, GateRunRed, true},
		{"a timeout", GateTriggerPerson, []GateResult{{Kind: GateTest, ExitCode: -1, TimedOut: true}}, nil, GateRunRed, false},
		{"missing wins over green gates", GateTriggerDeclaration, []GateResult{ok}, []GateKind{GateSecretScan}, GateRunNotPassed, true},
		{"practice never counts", GateTriggerPractice, []GateResult{{Kind: GateTest, ExitCode: 2}}, nil, GateRunRed, false},
	}
	for _, c := range cases {
		r := &GateRun{Trigger: c.trigger, Gates: c.gates, MissingKinds: c.missing}
		r.Finish(time.Now())
		if r.Result != c.result || r.Counted != c.counted {
			t.Errorf("%s: result %q counted %v, want %q %v", c.name, r.Result, r.Counted, c.result, c.counted)
		}
	}
}

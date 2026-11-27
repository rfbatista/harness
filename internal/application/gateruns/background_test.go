package gateruns

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestAGateThatLeavesABackgroundProcessKeepsItsOwnExitCodeAndNamesTheLeftover(t *testing.T) {
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{
		domain.GateBuild: "sleep 30 & echo built",
		domain.GateTest:  "sleep 30 & echo failing; exit 3",
	}, 10)
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPractice,
		Kinds: []domain.GateKind{domain.GateBuild, domain.GateTest}})
	if err != nil {
		t.Fatal(err)
	}
	build, test := run.Gates[0], run.Gates[1]
	if build.ExitCode != 0 || !build.Passed() || !strings.Contains(build.Output, "background process") {
		t.Errorf("a passing gate with a leftover: %+v", build)
	}
	if test.ExitCode != 3 || test.Passed() || !strings.Contains(test.Output, "background process") {
		t.Errorf("a failing gate keeps its own exit code: %+v", test)
	}
	if run.Result != domain.GateRunRed {
		t.Errorf("run = %s", run.Result)
	}
}

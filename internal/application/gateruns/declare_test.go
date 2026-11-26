package gateruns

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

type fixedCheck struct {
	refuse bool
	code   string
	msg    string
	counts bool
	calls  int
}

func (f *fixedCheck) Check(context.Context, *domain.Session) (CheckResult, error) {
	f.calls++
	return CheckResult{Refused: f.refuse, Code: f.code, Message: f.msg, Counts: f.counts}, nil
}

func declarer(mode domain.SessionMode, mutate func(*domain.GuardConfig), hasDiff bool) (*Declarer, *scriptedExec) {
	svc, exec := runner(mode, mutate, hasDiff)
	return NewDeclarer(svc), exec
}

func TestAGreenGateRunAcceptsTheDeclarationAndARedOneRefusesItWithTheFailingOutput(t *testing.T) {
	d, exec := declarer(domain.SessionModeImplement, nil, false)
	exec.exits["test"] = 1
	res, err := d.Declare(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted || len(res.Refusals) != 1 || res.Refusals[0].Code != "GATES_RED" || !res.Refusals[0].Counts {
		t.Fatalf("declaration = %+v", res)
	}
	msg := res.Refusals[0].Message
	if !strings.Contains(msg, "test") || !strings.Contains(msg, "exit code 1") || !strings.Contains(msg, "ran test") {
		t.Errorf("the refusal does not carry the failing gate's command, exit code and output: %q", msg)
	}
	if res.Run == nil || res.Run.Trigger != domain.GateTriggerDeclaration || !res.Run.Counted {
		t.Errorf("run = %+v", res.Run)
	}

	exec.exits["test"] = 0
	res, err = d.Declare(context.Background(), "s")
	if err != nil || !res.Accepted || len(res.Refusals) != 0 {
		t.Errorf("after the fix: %+v, %v", res, err)
	}
}

func TestAMissingRequiredGateRefusesTheDeclarationNamingTheKinds(t *testing.T) {
	d, exec := declarer(domain.SessionModeImplement, func(c *domain.GuardConfig) {
		c.Gates[3].Command = ""
		c.MissingRequiredGates = []domain.GateKind{domain.GateSecretScan}
	}, false)
	res, _ := d.Declare(context.Background(), "s")
	if res.Accepted || len(res.Refusals) != 1 || res.Refusals[0].Code != "REQUIRED_GATE_MISSING" || !strings.Contains(res.Refusals[0].Message, "secret_scan") {
		t.Errorf("declaration = %+v", res)
	}
	if len(exec.ran) != 0 {
		t.Errorf("ran %v", exec.ran)
	}
}

func TestANonGatedSessionsDeclarationIsAcceptedAndRunsNothing(t *testing.T) {
	d, exec := declarer(domain.SessionModeReview, nil, true)
	res, err := d.Declare(context.Background(), "s")
	if err != nil || !res.Accepted || res.Run != nil || len(exec.ran) != 0 {
		t.Errorf("declaration = %+v, %v, ran %v", res, err, exec.ran)
	}
}

func TestAnotherDeclarationCheckThatRefusesIsReportedTogetherWithAGreenGateRun(t *testing.T) {
	d, _ := declarer(domain.SessionModeImplement, nil, false)
	memory := &fixedCheck{refuse: true, code: "TASK_MEMORY_STALE", msg: "the task memory was not updated"}
	d.Add(memory)
	res, _ := d.Declare(context.Background(), "s")
	if res.Accepted || len(res.Refusals) != 1 || res.Refusals[0].Code != "TASK_MEMORY_STALE" || res.Refusals[0].Counts {
		t.Fatalf("declaration = %+v", res)
	}
	if res.Run == nil || !res.Run.Green() {
		t.Errorf("the gate run should still have run green: %+v", res.Run)
	}
}

func TestARedGateRunAndARefusingCheckAreBothInTheAnswer(t *testing.T) {
	d, exec := declarer(domain.SessionModeImplement, nil, false)
	exec.exits["lint"] = 3
	d.Add(&fixedCheck{refuse: true, code: "TASK_MEMORY_STALE", msg: "the task memory was not updated"})
	res, _ := d.Declare(context.Background(), "s")
	if len(res.Refusals) != 2 || res.Refusals[0].Code != "GATES_RED" || res.Refusals[1].Code != "TASK_MEMORY_STALE" {
		t.Errorf("refusals = %+v", res.Refusals)
	}
	if msg := res.Message(); !strings.Contains(msg, "lint") || !strings.Contains(msg, "task memory") {
		t.Errorf("message = %q", msg)
	}
}

func TestACheckThatPassesChangesNothing(t *testing.T) {
	d, _ := declarer(domain.SessionModeImplement, nil, false)
	ok := &fixedCheck{}
	d.Add(ok)
	res, _ := d.Declare(context.Background(), "s")
	if !res.Accepted || ok.calls != 1 {
		t.Errorf("declaration = %+v, calls %d", res, ok.calls)
	}
}

func TestAPracticeRunIsRecordedWithoutCounting(t *testing.T) {
	d, exec := declarer(domain.SessionModeImplement, nil, false)
	exec.exits["test"] = 1
	run, err := d.Practice(context.Background(), "s", []domain.GateKind{domain.GateTest})
	if err != nil {
		t.Fatal(err)
	}
	if run.Trigger != domain.GateTriggerPractice || run.Counted || run.Result != domain.GateRunRed || len(exec.ran) != 1 {
		t.Errorf("run = %+v ran %v", run, exec.ran)
	}
}

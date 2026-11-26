package gateruns

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
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

type recordedEscalation struct {
	sessions []string
	runs     []*domain.GateRun
}

func (r *recordedEscalation) Escalate(_ context.Context, sess *domain.Session, run *domain.GateRun) error {
	r.sessions = append(r.sessions, sess.ID)
	r.runs = append(r.runs, run)
	return nil
}

func escalating(limit int) (*Declarer, *scriptedExec, *recordedEscalation) {
	reds["s"] = 0
	d, exec := declarer(domain.SessionModeImplement, func(c *domain.GuardConfig) { c.RetryLimit = limit }, false)
	esc := &recordedEscalation{}
	d.Escalation = esc
	return d, exec, esc
}

func declareN(t *testing.T, d *Declarer, n int) ports.Declaration {
	t.Helper()
	var res ports.Declaration
	for i := 0; i < n; i++ {
		var err error
		if res, err = d.Declare(context.Background(), "s"); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func TestTheDeclarationThatReachesTheRetryLimitEscalatesAndEarlierRedOnesDoNot(t *testing.T) {
	d, exec, esc := escalating(3)
	exec.exits["test"] = 1
	res := declareN(t, d, 2)
	if len(esc.sessions) != 0 || !strings.Contains(res.Message(), "2 of 3") {
		t.Fatalf("after two red declarations: escalated %v, message %q", esc.sessions, res.Message())
	}
	res = declareN(t, d, 1)
	if len(esc.sessions) != 1 || !esc.runs[0].Escalated || len(esc.runs[0].RedKinds()) != 1 || esc.runs[0].RedKinds()[0] != domain.GateTest {
		t.Errorf("the third red declaration: escalated %v runs %+v", esc.sessions, esc.runs)
	}
	if res.Accepted {
		t.Error("the escalating declaration was accepted")
	}
}

func TestAGreenDeclarationResetsTheCountAndPracticeAndPersonRunsNeverCount(t *testing.T) {
	d, exec, esc := escalating(3)
	exec.exits["test"] = 1
	declareN(t, d, 2)
	for i := 0; i < 3; i++ {
		_, _ = d.Practice(context.Background(), "s", nil)
		_, _ = d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson})
	}
	if reds["s"] != 2 {
		t.Fatalf("practice and person runs changed the count: %d", reds["s"])
	}
	exec.exits["test"] = 0
	if res := declareN(t, d, 1); !res.Accepted {
		t.Fatal("green was refused")
	}
	exec.exits["test"] = 1
	declareN(t, d, 1)
	if len(esc.sessions) != 0 || reds["s"] != 1 {
		t.Errorf("red, red, green, red escalated %v with count %d", esc.sessions, reds["s"])
	}
}

func TestALimitOfOneEscalatesOnTheFirstRedDeclarationAndMissingGatesNeverCount(t *testing.T) {
	d, exec, esc := escalating(1)
	exec.exits["build"] = 1
	declareN(t, d, 1)
	if len(esc.sessions) != 1 {
		t.Errorf("limit 1: escalated %v", esc.sessions)
	}

	d, _, esc = escalating(1)
	d.runs.d.Guard = fakeGuard{"s": domain.GuardConfig{Mode: domain.SessionModeImplement, Gates: gates(), RetryLimit: 1,
		MissingRequiredGates: []domain.GateKind{domain.GateSecretScan}}}
	declareN(t, d, 2)
	if len(esc.sessions) != 0 {
		t.Errorf("a missing required gate escalated: %v", esc.sessions)
	}
}

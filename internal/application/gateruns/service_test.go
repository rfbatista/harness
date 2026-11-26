package gateruns

import (
	"context"
	"io"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeSessions map[string]*domain.Session

func (f fakeSessions) Find(_ context.Context, id string) (*domain.Session, error) { return f[id], nil }

type fakeGuard map[string]domain.GuardConfig

func (f fakeGuard) Keep(_ context.Context, id string, c domain.GuardConfig) error { f[id] = c; return nil }
func (f fakeGuard) Config(_ context.Context, id string) (domain.GuardConfig, bool, error) {
	c, ok := f[id]
	return c, ok, nil
}

type memStore struct{ runs []domain.GateRun }

func (m *memStore) Save(_ context.Context, r *domain.GateRun) error { m.runs = append(m.runs, *r); return nil }
func (m *memStore) List(context.Context, string) ([]domain.GateRun, error) { return m.runs, nil }

// scriptedExec answers each command with an exit code and records the order.
type scriptedExec struct {
	exits map[string]int
	ran   []string
}

func (s *scriptedExec) Execute(_ context.Context, _ *domain.Session, command string, _ time.Duration, out io.Writer) ports.GateExit {
	s.ran = append(s.ran, command)
	_, _ = io.WriteString(out, "ran "+command)
	return ports.GateExit{ExitCode: s.exits[command]}
}

func gates() []domain.GuardGate {
	var out []domain.GuardGate
	for _, k := range domain.RequiredGateKinds {
		out = append(out, domain.GuardGate{Kind: k, Command: string(k), TimeoutSeconds: 60})
	}
	return out
}

func runner(mode domain.SessionMode, mutate func(*domain.GuardConfig), hasDiff bool) (*Service, *scriptedExec) {
	cfg := domain.GuardConfig{Mode: mode, Gates: gates(), RetryLimit: 3}
	if mutate != nil {
		mutate(&cfg)
	}
	exec := &scriptedExec{exits: map[string]int{}}
	return New(Deps{
		Sessions: fakeSessions{"s": {ID: "s"}}, Guard: fakeGuard{"s": cfg}, Store: &memStore{}, Exec: exec,
		HasDiff: func(context.Context, *domain.Session) bool { return hasDiff },
	}), exec
}

func TestOnlyGatedSessionsRunGates(t *testing.T) {
	cases := []struct {
		mode    domain.SessionMode
		hasDiff bool
		gated   bool
	}{
		{domain.SessionModeImplement, false, true},
		{domain.SessionModeTest, false, true},
		{domain.SessionModeDefault, true, true},
		{domain.SessionModeDefault, false, false},
		{domain.SessionModeArchitect, true, false},
		{domain.SessionModeReview, true, false},
	}
	for _, c := range cases {
		svc, exec := runner(c.mode, nil, c.hasDiff)
		_, err := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson})
		if (err == nil) != c.gated {
			t.Errorf("mode %q diff %v: err = %v, want gated %v", c.mode, c.hasDiff, err, c.gated)
		}
		if !c.gated && len(exec.ran) != 0 {
			t.Errorf("mode %q ran %v", c.mode, exec.ran)
		}
	}
}

func TestASessionThatKeptNoConfigurationOrAnUnresolvedOneIsNotGated(t *testing.T) {
	svc, _ := runner(domain.SessionModeImplement, func(c *domain.GuardConfig) { c.Unresolved = "no store" }, false)
	if _, err := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s"}); err == nil {
		t.Error("an unresolved configuration was gated")
	}
	svc.d.Guard = fakeGuard{}
	if _, err := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s"}); err == nil {
		t.Error("a session with no kept configuration was gated")
	}
	if _, err := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "nope"}); err == nil {
		t.Error("an unknown session ran")
	}
}

func TestEveryGateRunsEvenWhenAnEarlierOneIsRed(t *testing.T) {
	svc, exec := runner(domain.SessionModeImplement, nil, false)
	exec.exits["build"] = 2
	run, err := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerDeclaration})
	if err != nil {
		t.Fatal(err)
	}
	if run.Result != domain.GateRunRed || len(exec.ran) != 5 || !run.Counted || len(run.Failing()) != 1 {
		t.Errorf("run %q ran %v counted %v failing %v", run.Result, exec.ran, run.Counted, run.Failing())
	}
}

func TestAKindFilterRunsOnlyThoseGates(t *testing.T) {
	svc, exec := runner(domain.SessionModeImplement, nil, false)
	run, _ := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPractice, Kinds: []domain.GateKind{domain.GateTest}})
	if len(exec.ran) != 1 || exec.ran[0] != "test" || run.Counted || len(run.Kinds) != 1 {
		t.Errorf("ran %v counted %v kinds %v", exec.ran, run.Counted, run.Kinds)
	}
}

func TestAMissingRequiredGateEndsNotPassedWithoutRunningAnything(t *testing.T) {
	missing := func(c *domain.GuardConfig) {
		c.Gates[3].Command = ""
		c.MissingRequiredGates = []domain.GateKind{domain.GateSecretScan}
	}
	svc, exec := runner(domain.SessionModeImplement, missing, false)
	run, _ := svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s"})
	if run.Result != domain.GateRunNotPassed || len(exec.ran) != 0 || len(run.MissingKinds) != 1 {
		t.Errorf("run %+v ran %v", run, exec.ran)
	}
	// A practice run limited to a gate that is present is not held up by it.
	svc, exec = runner(domain.SessionModeImplement, missing, false)
	run, _ = svc.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Kinds: []domain.GateKind{domain.GateBuild}})
	if run.Result != domain.GateRunGreen || len(exec.ran) != 1 {
		t.Errorf("filtered run %+v ran %v", run, exec.ran)
	}
}

func TestAGatesOutputIsKeptAsItsTail(t *testing.T) {
	b := &tailBuffer{max: 4}
	_, _ = b.Write([]byte("abcdef"))
	_, _ = b.Write([]byte("gh"))
	if b.String() != "efgh" {
		t.Errorf("tail = %q", b.String())
	}
}

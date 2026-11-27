//go:build !windows

package gateruns

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Adversarial tests of the gate runner and the done declaration (seam A):
// a real shell for the hanging and the noisy gate, and sequences around the
// retry limit.

// safeGuard counts red declarations under a lock, like the SQLite store does.
type safeGuard struct {
	mu  sync.Mutex
	cfg domain.GuardConfig
	red int
}

func (g *safeGuard) Keep(context.Context, string, domain.GuardConfig) error { return nil }
func (g *safeGuard) Config(context.Context, string) (domain.GuardConfig, bool, error) {
	return g.cfg, true, nil
}
func (g *safeGuard) CountRed(context.Context, string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.red++
	return g.red, nil
}
func (g *safeGuard) MarkEscalated(context.Context, string) error { return nil }
func (g *safeGuard) ResumeRed(context.Context, string) error     { return nil }
func (g *safeGuard) ResetRed(context.Context, string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.red = 0
	return nil
}

type lockedStore struct {
	mu   sync.Mutex
	runs []domain.GateRun
}

func (m *lockedStore) Save(_ context.Context, r *domain.GateRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs = append(m.runs, *r)
	return nil
}
func (m *lockedStore) List(context.Context, string) ([]domain.GateRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.GateRun(nil), m.runs...), nil
}

type countingEscalation struct {
	mu sync.Mutex
	n  int
}

func (c *countingEscalation) Escalate(context.Context, *domain.Session, *domain.GateRun) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return nil
}

// gateOverrides replaces the command of some kinds; the others are "true".
func shellRunner(t *testing.T, limit int, override map[domain.GateKind]string, timeout int) (*Declarer, *safeGuard, *countingEscalation) {
	t.Helper()
	var gs []domain.GuardGate
	for _, k := range domain.RequiredGateKinds {
		cmd := "true"
		if o, ok := override[k]; ok {
			cmd = o
		}
		gs = append(gs, domain.GuardGate{Kind: k, Command: cmd, TimeoutSeconds: timeout})
	}
	guard := &safeGuard{cfg: domain.GuardConfig{Mode: domain.SessionModeImplement, Gates: gs, RetryLimit: limit}}
	svc := New(Deps{
		Sessions: fakeSessions{"s": {ID: "s", WorkingDir: t.TempDir()}}, Guard: guard, Store: &lockedStore{},
		Exec: &ShellExecutor{},
	})
	d := NewDeclarer(svc)
	esc := &countingEscalation{}
	d.Escalation = esc
	return d, guard, esc
}

func TestAGateThatHangsIsKilledAtItsTimeoutAndTheRunIsRed(t *testing.T) {
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{domain.GateBuild: "echo started; sleep 60"}, 1)
	start := time.Now()
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson, Kinds: []domain.GateKind{domain.GateBuild}})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 8*time.Second {
		t.Errorf("a 1s gate held the run for %v", took)
	}
	g := run.Gates[0]
	if run.Result != domain.GateRunRed || !g.TimedOut || g.Passed() || !strings.Contains(g.Output, "started") || !strings.Contains(g.Output, "timed out") {
		t.Errorf("run = %s, gate = %+v", run.Result, g)
	}
	if rep := run.FailureReport(); !strings.Contains(rep, "timed out after 1s") {
		t.Errorf("report = %q", rep)
	}
}

func TestAGateThatLeavesAChildHoldingItsOutputDoesNotHangTheRun(t *testing.T) {
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{domain.GateBuild: "sleep 30 & echo built"}, 10)
	start := time.Now()
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson, Kinds: []domain.GateKind{domain.GateBuild}})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 8*time.Second {
		t.Errorf("the run waited %v for a background child", took)
	}
	// The gate itself exited 0 and printed "built": the run is green. A
	// background process the gate left behind is not a failure of the gate.
	if g := run.Gates[0]; g.ExitCode != 0 || run.Result != domain.GateRunGreen {
		t.Errorf("a gate that exited 0 with a lingering child: run %s, gate exit %d, output %q", run.Result, g.ExitCode, g.Output)
	}
}

func TestAGateThatWritesHugeOutputIsKeptAsItsBoundedTail(t *testing.T) {
	big := "i=0; while [ $i -lt 40 ]; do head -c 1048576 /dev/zero | tr '\\0' 'x'; i=$((i+1)); done; echo THE-END; exit 1"
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{domain.GateTest: big}, 60)
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson, Kinds: []domain.GateKind{domain.GateTest}})
	if err != nil {
		t.Fatal(err)
	}
	g := run.Gates[0]
	if len(g.Output) > domain.MaxGateOutputBytes {
		t.Errorf("stored %d bytes of output, cap %d", len(g.Output), domain.MaxGateOutputBytes)
	}
	if !strings.HasSuffix(strings.TrimSpace(g.Output), "THE-END") || g.ExitCode != 1 || run.Result != domain.GateRunRed {
		t.Errorf("tail lost: exit %d, result %s, ends %q", g.ExitCode, run.Result, g.Output[max(0, len(g.Output)-20):])
	}
	// What goes back to the agent is bounded and valid text.
	if rep := run.FailureReport(); len(rep) > domain.MaxReportedOutputBytes+512 || !utf8.ValidString(rep) {
		t.Errorf("the failure report is %d bytes, valid utf-8 %v", len(rep), utf8.ValidString(rep))
	}
}

func TestAGateThatWritesMultibyteOutputGivesTheAgentValidText(t *testing.T) {
	cmd := "i=0; while [ $i -lt 3000 ]; do printf '€€€€€€€€€'; i=$((i+1)); done; exit 2"
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{domain.GateLint: cmd}, 60)
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson, Kinds: []domain.GateKind{domain.GateLint}})
	if err != nil {
		t.Fatal(err)
	}
	if rep := run.FailureReport(); !utf8.ValidString(rep) {
		t.Errorf("the failure report handed to the agent is not valid UTF-8: tail %q", rep[max(0, len(rep)-12):])
	}
}

func TestAGateRunsInTheWorktreeAndAGateThatFailsToStartIsRedNotAnError(t *testing.T) {
	d, _, _ := shellRunner(t, 3, map[domain.GateKind]string{
		domain.GateBuild: "pwd", domain.GateLint: "definitely-not-a-command-xyz"}, 10)
	run, err := d.runs.Run(context.Background(), ports.GateRunRequest{SessionID: "s", Trigger: domain.GateTriggerPerson})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range run.Gates {
		switch g.Kind {
		case domain.GateBuild:
			if !strings.Contains(g.Output, "/") || g.ExitCode != 0 {
				t.Errorf("build = %+v", g)
			}
		case domain.GateLint:
			if g.ExitCode == 0 || g.Passed() {
				t.Errorf("a missing command passed: %+v", g)
			}
		}
	}
	if run.Result != domain.GateRunRed {
		t.Errorf("result = %s", run.Result)
	}
}

func setExit(g *safeGuard, kind domain.GateKind, command string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := range g.cfg.Gates {
		if g.cfg.Gates[i].Kind == kind {
			g.cfg.Gates[i].Command = command
		}
	}
}

func TestRedGreenRedSequencesCountOnlyConsecutiveRedDeclarations(t *testing.T) {
	d, g, esc := shellRunner(t, 3, nil, 10)
	ctx := context.Background()
	declare := func(want bool) {
		t.Helper()
		res, err := d.Declare(ctx, "s")
		if err != nil || res.Accepted != want {
			t.Fatalf("declare: accepted %v err %v, want accepted %v", res.Accepted, err, want)
		}
	}
	red := func() { setExit(g, domain.GateTest, "exit 1") }
	green := func() { setExit(g, domain.GateTest, "true") }

	// red red green red red green: never three in a row.
	red()
	declare(false)
	declare(false)
	green()
	declare(true)
	red()
	declare(false)
	declare(false)
	green()
	declare(true)
	if esc.n != 0 {
		t.Fatalf("escalated %d times without three consecutive reds", esc.n)
	}
	// A practice run between reds, red or green, neither counts nor resets.
	red()
	declare(false)
	declare(false)
	if _, err := d.Practice(ctx, "s", nil); err != nil {
		t.Fatal(err)
	}
	green()
	if _, err := d.Practice(ctx, "s", nil); err != nil {
		t.Fatal(err)
	}
	if g.red != 2 {
		t.Errorf("practice runs changed the count to %d", g.red)
	}
	red()
	declare(false)
	if esc.n != 1 {
		t.Errorf("the third consecutive red declaration escalated %d times, want 1", esc.n)
	}
}

func TestARetryLimitBelowTheMinimumIsRaisedToTheMinimum(t *testing.T) {
	for _, limit := range []int{0, -5} {
		d, g, esc := shellRunner(t, limit, map[domain.GateKind]string{domain.GateTest: "exit 1"}, 10)
		for i := 0; i < domain.MinRetryLimit-1; i++ {
			if _, err := d.Declare(context.Background(), "s"); err != nil {
				t.Fatal(err)
			}
		}
		if esc.n != 0 {
			t.Errorf("limit %d escalated after %d reds (count %d)", limit, domain.MinRetryLimit-1, g.red)
		}
		if _, err := d.Declare(context.Background(), "s"); err != nil {
			t.Fatal(err)
		}
		if esc.n != 1 {
			t.Errorf("limit %d: %d escalations at the minimum", limit, esc.n)
		}
	}
}

func TestTwoDeclarationsAtOnceEscalateTheSessionOnceAndNotifyOnce(t *testing.T) {
	d, g, esc := shellRunner(t, 3, map[domain.GateKind]string{domain.GateTest: "exit 1"}, 10)
	for i := 0; i < 2; i++ {
		if _, err := d.Declare(context.Background(), "s"); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res, err := d.Declare(context.Background(), "s"); err != nil || res.Accepted {
				t.Errorf("declare: %+v %v", res, err)
			}
		}()
	}
	wg.Wait()
	if esc.n != 1 {
		t.Errorf("two simultaneous red declarations at the limit escalated %d times (count %d); the person gets one notification per escalation", esc.n, g.red)
	}
}

func TestADeclarationAfterEscalationDoesNotEscalateAgainWithoutNewReds(t *testing.T) {
	// A stopped session cannot declare; a resumed one starts again. The count
	// is not reset by an escalation or a resume, so the first red after the
	// resume is the fourth and escalates at once. This pins the behaviour.
	d, g, esc := shellRunner(t, 3, map[domain.GateKind]string{domain.GateTest: "exit 1"}, 10)
	for i := 0; i < 3; i++ {
		if _, err := d.Declare(context.Background(), "s"); err != nil {
			t.Fatal(err)
		}
	}
	if esc.n != 1 || g.red != 3 {
		t.Fatalf("setup: escalations %d count %d", esc.n, g.red)
	}
	if _, err := d.Declare(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if esc.n != 2 {
		t.Logf("the first red after a resume escalated again: %d escalations, count %d", esc.n, g.red)
	}
}

func TestAMissingRequiredGateEndsNotPassedWithoutHangingEvenWhenOthersWouldHang(t *testing.T) {
	d, g, esc := shellRunner(t, 3, map[domain.GateKind]string{domain.GateBuild: "sleep 60"}, 1)
	g.cfg.MissingRequiredGates = []domain.GateKind{domain.GateSecretScan}
	start := time.Now()
	res, err := d.Declare(context.Background(), "s")
	if err != nil || res.Accepted || res.Run.Result != domain.GateRunNotPassed {
		t.Fatalf("declare = %+v, %v", res, err)
	}
	if time.Since(start) > 3*time.Second || esc.n != 0 || g.red != 0 {
		t.Errorf("took %v, escalated %d, counted %d", time.Since(start), esc.n, g.red)
	}
	if !strings.Contains(res.Message(), "secret_scan") {
		t.Errorf("message = %q", res.Message())
	}
}

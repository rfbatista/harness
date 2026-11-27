package archrules

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Adversarial tests of the Guard rules (seam A) through CheckArchitecture:
// symlinks, ../ paths, names with spaces, lockfile and manifest pairs, and
// tests-fail-first with test files in unusual places.

type keptGuard struct{ cfg domain.GuardConfig }

func (k keptGuard) Keep(context.Context, string, domain.GuardConfig) error { return nil }
func (k keptGuard) Config(context.Context, string) (domain.GuardConfig, bool, error) {
	return k.cfg, true, nil
}
func (k keptGuard) CountRed(context.Context, string) (int, error) { return 0, nil }
func (k keptGuard) MarkEscalated(context.Context, string) error   { return nil }
func (k keptGuard) ResumeRed(context.Context, string) error       { return nil }
func (k keptGuard) ResetRed(context.Context, string) error        { return nil }

type memRuleEvents struct {
	events []*domain.ArchitectureRuleEvent
}

func (m *memRuleEvents) Add(_ context.Context, ev *domain.ArchitectureRuleEvent) error {
	m.events = append(m.events, ev)
	return nil
}
func (m *memRuleEvents) List(context.Context, string, time.Time, int) ([]domain.ArchitectureRuleEvent, error) {
	return nil, nil
}
func (m *memRuleEvents) DeleteBySession(context.Context, string) error { return nil }

type fixedRuns struct {
	runs []domain.GateRun
	err  error
}

func (f fixedRuns) Save(context.Context, *domain.GateRun) error { return nil }
func (f fixedRuns) List(context.Context, string) ([]domain.GateRun, error) {
	return f.runs, f.err
}

type guardWorld struct {
	wt     string
	events *memRuleEvents
	svc    *Service
}

func newGuardWorld(t *testing.T, cfg domain.GuardConfig, mutate func(*Deps)) *guardWorld {
	t.Helper()
	// EvalSymlinks of the temp dir: on macOS /var is a link to /private/var.
	wt, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode == "" {
		cfg.Mode = domain.SessionModeImplement
	}
	ev := &memRuleEvents{}
	d := Deps{
		Records:  noRecords{},
		Sessions: sessionsByID{"s": {ID: "s", RepositoryID: "r", ProjectID: "p", WorkingDir: wt, Access: domain.AccessWrite}},
		Events:   ev,
		Guard:    keptGuard{cfg},
	}
	if mutate != nil {
		mutate(&d)
	}
	return &guardWorld{wt: wt, events: ev, svc: NewService(d)}
}

func (w *guardWorld) write(rel, body string) string {
	p := filepath.Join(w.wt, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		panic(err)
	}
	return p
}

func (w *guardWorld) edit(path string) ports.ArchitectureVerdict {
	return w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{path}})
}

func (w *guardWorld) shell(command string, targets ...string) ports.ArchitectureVerdict {
	return w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointBeforeTool, Tool: "Bash", Command: command, ShellWrites: targets})
}

var defaultProtected = []string{"check.sh", ".github/workflows/**", "ci/**"}

func TestAProtectedFileIsRefusedThroughASymlinkInsideTheWorktree(t *testing.T) {
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: defaultProtected}, nil)
	w.write("check.sh", "ok\n")
	w.write(".github/workflows/ci.yml", "x\n")
	if err := os.Symlink("check.sh", filepath.Join(w.wt, "link.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".github/workflows", filepath.Join(w.wt, "wf")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../check.sh", filepath.Join(w.wt, "sub-link")); err != nil { // dangling
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(w.wt, "check.sh"), filepath.Join(w.wt, "abs-link.sh")); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]string{
		"a file link":        filepath.Join(w.wt, "link.sh"),
		"an absolute link":   filepath.Join(w.wt, "abs-link.sh"),
		"a folder link":      filepath.Join(w.wt, "wf", "ci.yml"),
		"a new file in link": filepath.Join(w.wt, "wf", "new.yml"),
		"a nested new file":  filepath.Join(w.wt, "wf", "a", "b", "new.yml"),
	} {
		if v := w.edit(p); !strings.Contains(v.Deny, "PROTECTED_PATH") {
			t.Errorf("%s: %s not refused: %+v", name, p, v)
		}
	}
	// The shell route (a redirect) resolves links the same way.
	if v := w.shell("echo x > link.sh", filepath.Join(w.wt, "link.sh")); !strings.Contains(v.Deny, "PROTECTED_PATH") {
		t.Errorf("a redirect through a link: %+v", v)
	}
}

func TestAProtectedFileIsRefusedThroughDotDotAndDotSegmentsAndNamesWithSpaces(t *testing.T) {
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: append([]string{"my dir/**"}, defaultProtected...)}, nil)
	w.write("check.sh", "ok\n")
	w.write("sub/x.go", "package x\n")
	for name, p := range map[string]string{
		"dot-dot inside":      filepath.Join(w.wt, "sub", "..", "check.sh"),
		"dot-dot via missing": filepath.Join(w.wt, "nope", "..", "check.sh"),
		"dot segment":         filepath.Join(w.wt, ".", "check.sh"),
		"relative":            "check.sh",
		"relative dot-dot":    "sub/../check.sh",
		"a space-named dir":   filepath.Join(w.wt, "my dir", "a b.txt"),
		"a double slash":      w.wt + "//check.sh",
		"a trailing dot":      filepath.Join(w.wt, "ci", ".", "x.sh"),
	} {
		if v := w.edit(p); !strings.Contains(v.Deny, "PROTECTED_PATH") {
			t.Errorf("%s: %q not refused: %+v", name, p, v)
		}
	}
	for name, p := range map[string]string{
		"a sibling with a shared prefix": filepath.Join(w.wt, "check.sh.bak"),
		"unprotected code":               filepath.Join(w.wt, "sub", "x.go"),
		"a dir named like the glob head": filepath.Join(w.wt, "ci-tools", "x.sh"),
		"outside the worktree":           filepath.Join(filepath.Dir(w.wt), "check.sh"),
		"climbing out then in elsewhere": filepath.Join(w.wt, "..", "elsewhere", "check.sh"),
	} {
		if v := w.edit(p); v.Deny != "" {
			t.Errorf("%s: %q refused: %q", name, p, v.Deny)
		}
	}
}

func TestAProtectedWriteIsRefusedOnlyBeforeTheToolAndEachRefusalIsRecordedOnTheSession(t *testing.T) {
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: defaultProtected}, nil)
	w.write("check.sh", "ok\n")
	if v := w.edit(filepath.Join(w.wt, "check.sh")); v.Deny == "" {
		t.Fatal("not refused")
	}
	if len(w.events.events) != 1 || w.events.events[0].SessionID != "s" || w.events.events[0].RuleID != ProtectedPathRule ||
		w.events.events[0].Outcome != domain.OutcomeBlocked || w.events.events[0].Path != "check.sh" {
		t.Errorf("events = %+v", w.events.events)
	}
	// Two shell targets, one protected: the refusal names the protected one.
	v := w.shell("cp a b", filepath.Join(w.wt, "notes.txt"), filepath.Join(w.wt, "ci", "x.sh"))
	if !strings.Contains(v.Deny, "ci/x.sh") || !strings.Contains(v.Deny, "ci/**") {
		t.Errorf("deny = %q", v.Deny)
	}
}

func TestAPersonsOrANonGatedSessionsWritesAreNeverRefusedAndAConfigurationThatCannotBeReadRefusesNothingOpenByMistake(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeArchitect, domain.SessionModeReview, domain.SessionModeQA,
		domain.SessionModeOperate, domain.SessionModeProduct} {
		w := newGuardWorld(t, domain.GuardConfig{Mode: mode, ProtectedPaths: defaultProtected}, nil)
		if v := w.edit(filepath.Join(w.wt, "check.sh")); v.Deny != "" {
			t.Errorf("mode %q was refused: %q", mode, v.Deny)
		}
	}
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: defaultProtected}, nil)
	v := w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Person: true, Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{filepath.Join(w.wt, "check.sh")}})
	if v.Deny != "" {
		t.Errorf("the person's own write was refused: %q", v.Deny)
	}
	// An unknown session id and an empty one are not guarded: nothing to apply.
	for _, id := range []string{"", "nobody"} {
		v := w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
			SessionID: id, Point: domain.PointBeforeTool, Tool: "Write", Paths: []string{filepath.Join(w.wt, "check.sh")}})
		if v.Deny != "" {
			t.Errorf("session %q refused: %q", id, v.Deny)
		}
	}
}

func TestASessionScopeUnlocksOnlyWhatItNamesEvenThroughASymlink(t *testing.T) {
	w := newGuardWorld(t, domain.GuardConfig{ProtectedPaths: defaultProtected, Scope: []string{"ci/**"}}, nil)
	w.write("check.sh", "ok\n")
	w.write("ci/build.sh", "x\n")
	if err := os.Symlink("../check.sh", filepath.Join(w.wt, "ci", "to-check")); err != nil {
		t.Fatal(err)
	}
	if v := w.edit(filepath.Join(w.wt, "ci", "build.sh")); v.Deny != "" {
		t.Errorf("a scoped path was refused: %q", v.Deny)
	}
	if v := w.edit(filepath.Join(w.wt, "check.sh")); v.Deny == "" {
		t.Error("an unscoped protected path was let through")
	}
	// A link inside the unlocked folder that points at a locked file is the
	// locked file.
	if v := w.edit(filepath.Join(w.wt, "ci", "to-check")); v.Deny == "" {
		t.Error("a symlink inside the scope reached a protected file outside it")
	}
}

func lockWorld(t *testing.T, changed map[string]bool, asked *[]string) *guardWorld {
	return newGuardWorld(t, domain.GuardConfig{LockfileRule: true}, func(d *Deps) {
		d.ManifestChanged = func(_ context.Context, _ *domain.Session, rel string) bool {
			if asked != nil {
				*asked = append(*asked, rel)
			}
			return changed[rel]
		}
	})
}

func TestALockfileMayOnlyFollowTheManifestBesideIt(t *testing.T) {
	var asked []string
	w := lockWorld(t, map[string]bool{"go.mod": true, "package.json": true}, &asked)
	w.write("sub/go.sum", "")
	w.write("web/yarn.lock", "")
	// The root go.mod changed; sub/go.sum rides on an unrelated manifest.
	for _, rel := range []string{"sub/go.sum", "sub/../sub/go.sum", "web/yarn.lock", "a b/go.sum", "pkg/Cargo.lock"} {
		v := w.edit(filepath.Join(w.wt, rel))
		if !strings.Contains(v.Deny, "LOCKFILE_WITHOUT_MANIFEST") {
			t.Errorf("%s with an unrelated manifest changed: %+v", rel, v)
		}
	}
	if len(asked) == 0 || !strings.Contains(strings.Join(asked, " "), "sub/go.mod") {
		t.Errorf("the pair of sub/go.sum was not asked about: %v", asked)
	}
	// The root pair passes, the shell route too.
	if v := w.edit(filepath.Join(w.wt, "go.sum")); v.Deny != "" {
		t.Errorf("go.sum with go.mod changed: %q", v.Deny)
	}
	if v := w.shell("echo x >> go.sum", filepath.Join(w.wt, "go.sum")); v.Deny != "" {
		t.Errorf("a redirect to go.sum with go.mod changed: %q", v.Deny)
	}
	if v := w.shell("echo x >> sub/go.sum", filepath.Join(w.wt, "sub", "go.sum")); !strings.Contains(v.Deny, "LOCKFILE_WITHOUT_MANIFEST") {
		t.Errorf("a redirect to sub/go.sum: %+v", v)
	}
}

func TestALockfileLinkIsTheLockfileAndAManifestLinkDoesNotFakeTheChange(t *testing.T) {
	w := lockWorld(t, nil, nil)
	w.write("go.sum", "")
	if err := os.Symlink("go.sum", filepath.Join(w.wt, "deps.txt")); err != nil {
		t.Fatal(err)
	}
	if v := w.edit(filepath.Join(w.wt, "deps.txt")); !strings.Contains(v.Deny, "LOCKFILE_WITHOUT_MANIFEST") {
		t.Errorf("a write through a link to go.sum: %+v", v)
	}
}

func TestTheLockfileRuleFlagsAfterTheEditOnlyWithTheManifestChangedAndNeverRefusesThen(t *testing.T) {
	w := lockWorld(t, map[string]bool{"go.mod": true}, nil)
	w.svc.d.Diff = func(context.Context, *domain.Session, string) string { return "+example.com/x v1.0.0 h1:a=\n" }
	v := w.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointAfterEdit, Tool: "Write", Paths: []string{filepath.Join(w.wt, "go.sum")}})
	if v.Deny != "" {
		t.Errorf("after_edit refused: %q", v.Deny)
	}
	if len(w.events.events) != 1 || w.events.events[0].RuleID != LockfileFlagRule || !strings.Contains(w.events.events[0].Detail, "example.com/x@v1.0.0") {
		t.Errorf("events = %+v", w.events.events)
	}
	// A lockfile edit with nothing added flags nothing.
	w2 := lockWorld(t, map[string]bool{"go.mod": true}, nil)
	w2.svc.d.Diff = func(context.Context, *domain.Session, string) string { return "-example.com/x v1.0.0 h1:a=\n" }
	w2.svc.CheckArchitecture(context.Background(), ports.ArchitectureCheck{
		SessionID: "s", Point: domain.PointAfterEdit, Tool: "Write", Paths: []string{filepath.Join(w2.wt, "go.sum")}})
	if len(w2.events.events) != 0 {
		t.Errorf("a removal was flagged: %+v", w2.events.events)
	}
}

func failedTest() domain.GateRun {
	return domain.GateRun{Trigger: domain.GateTriggerPractice, Result: domain.GateRunRed,
		Gates: []domain.GateResult{{Kind: domain.GateTest, ExitCode: 1}}}
}

func firstWorld(t *testing.T, runs fixedRuns) *guardWorld {
	return newGuardWorld(t, domain.GuardConfig{TestsFailFirstAvailable: true, TestsFailFirst: true}, func(d *Deps) { d.GateRuns = runs })
}

func TestTestsFailFirstJudgesEveryPathByWhereItLivesNotByWhatItIsCalledAlone(t *testing.T) {
	w := firstWorld(t, fixedRuns{})
	for _, rel := range []string{"main.go", "contest/main.go", "src/tester/x.go", "latest.go", "tests/../main.go", "a_test.go.bak", "a_test.go/real.go"} {
		if v := w.edit(filepath.Join(w.wt, rel)); !strings.Contains(v.Deny, "TESTS_MUST_FAIL_FIRST") {
			t.Errorf("%s was let through before a failed test: %+v", rel, v)
		}
	}
	for _, rel := range []string{"a_test.go", "pkg/x.test.ts", "tests/helper.go", "src/__tests__/x.js", "testdata/golden.json", "a b/tests/x.py"} {
		if v := w.edit(filepath.Join(w.wt, rel)); v.Deny != "" {
			t.Errorf("test file %s refused: %q", rel, v.Deny)
		}
	}
	// A write of a test and a non-test file together is refused.
	v := w.shell("cp a b", filepath.Join(w.wt, "a_test.go"), filepath.Join(w.wt, "main.go"))
	if !strings.Contains(v.Deny, "main.go") {
		t.Errorf("a mixed write: %+v", v)
	}
	// A symlink named like a test that points at code is code.
	w.write("main.go", "package main\n")
	if err := os.Symlink("main.go", filepath.Join(w.wt, "x_test.go")); err != nil {
		t.Fatal(err)
	}
	if v := w.edit(filepath.Join(w.wt, "x_test.go")); !strings.Contains(v.Deny, "TESTS_MUST_FAIL_FIRST") {
		t.Errorf("a test-named link to main.go let code through: %+v", v)
	}
}

func TestOnlyAFailedTestGateOpensTheWritesToCode(t *testing.T) {
	green := domain.GateRun{Trigger: domain.GateTriggerPractice, Result: domain.GateRunGreen,
		Gates: []domain.GateResult{{Kind: domain.GateTest, ExitCode: 0}}}
	redLint := domain.GateRun{Trigger: domain.GateTriggerPractice, Result: domain.GateRunRed,
		Gates: []domain.GateResult{{Kind: domain.GateTest, ExitCode: 0}, {Kind: domain.GateLint, ExitCode: 1}}}
	timedOut := domain.GateRun{Trigger: domain.GateTriggerPractice, Result: domain.GateRunRed,
		Gates: []domain.GateResult{{Kind: domain.GateTest, ExitCode: -1, TimedOut: true}}}
	notPassed := domain.GateRun{Trigger: domain.GateTriggerDeclaration, Result: domain.GateRunNotPassed, MissingKinds: []domain.GateKind{domain.GateTest}}
	for name, runs := range map[string][]domain.GateRun{
		"no runs": nil, "a green run": {green}, "a red lint beside a green test": {redLint}, "a run with a missing gate": {notPassed},
	} {
		w := firstWorld(t, fixedRuns{runs: runs})
		if v := w.edit(filepath.Join(w.wt, "main.go")); !strings.Contains(v.Deny, "TESTS_MUST_FAIL_FIRST") {
			t.Errorf("%s opened the writes: %+v", name, v)
		}
	}
	for name, run := range map[string]domain.GateRun{"a red test": failedTest(), "a test that timed out": timedOut} {
		w := firstWorld(t, fixedRuns{runs: []domain.GateRun{green, run}})
		if v := w.edit(filepath.Join(w.wt, "main.go")); v.Deny != "" {
			t.Errorf("%s did not open the writes: %q", name, v.Deny)
		}
	}
}

func TestTestsFailFirstRespectsItsSwitchesAndAStoreFaultNamesItsChoice(t *testing.T) {
	for name, cfg := range map[string]domain.GuardConfig{
		"on but unavailable": {TestsFailFirstAvailable: false, TestsFailFirst: true},
		"available but off":  {TestsFailFirstAvailable: true, TestsFailFirst: false},
	} {
		w := newGuardWorld(t, cfg, nil)
		if v := w.edit(filepath.Join(w.wt, "main.go")); v.Deny != "" {
			t.Errorf("%s: refused %q", name, v.Deny)
		}
	}
	// With the run list unreadable the rule lets the write through (the code
	// treats unknown as "a test failed"); pin it so a change is a decision.
	w := firstWorld(t, fixedRuns{err: errors.New("db down")})
	if v := w.edit(filepath.Join(w.wt, "main.go")); v.Deny != "" {
		t.Errorf("an unreadable run list refused: %q", v.Deny)
	}
}

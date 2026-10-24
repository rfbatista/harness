package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// sandboxWorld is a service whose project has three repositories, wired with
// a recording sandbox driver.
type sandboxWorld struct {
	svc    *Service
	driver *portstest.RecordingSandboxDriver
	repos  map[string]string // repository id -> checkout
	home   string
	extra  []ports.SandboxPath
	off    bool
	// readAll is the project's setting that agents read every repository; off
	// here, so these tests keep exercising the per-mode rules.
	readAll bool
	// wsRoot is the workspaces root the sandbox is told about.
	wsRoot string
}

func newSandboxWorld(t *testing.T) *sandboxWorld {
	t.Helper()
	svc, _, _ := newTestService(t)
	res := svc.catalog.Repositories.(*fakeResolver)
	w := &sandboxWorld{svc: svc, driver: &portstest.RecordingSandboxDriver{}, repos: map[string]string{}, home: t.TempDir()}
	res.others = nil
	for _, id := range []string{"r2", "r3"} {
		dir := t.TempDir()
		res.others = append(res.others, &domain.Repository{ID: id, ProjectID: "p1", Name: id, RootDir: dir})
		w.repos[id] = dir
	}
	w.repos["r1"] = res.repo.RootDir
	svc.Sandbox = &Sandbox{
		Driver:         w.driver,
		Enabled:        func() bool { return !w.off },
		Home:           w.home,
		Extra:          func() []ports.SandboxPath { return w.extra },
		WorkspacesRoot: func() string { return w.wsRoot },
		ReadAllRepos:   func(string) bool { return w.readAll },
	}
	w.wsRoot = t.TempDir()
	return w
}

func (w *sandboxWorld) start(t *testing.T, mode string) (*domain.Session, ports.SandboxPolicy) {
	t.Helper()
	req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode, RunsOn: domain.RunnerTUI}
	if mode != "product" {
		req.TicketID = "tk1"
	}
	sess, _, err := w.svc.StartInteractive(context.Background(), req)
	if err != nil {
		t.Fatalf("start %q: %v", mode, err)
	}
	calls := w.driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	return sess, calls[0].Policy
}

func TestSandbox_RepositoryVisibilityFollowsTheSessionMode(t *testing.T) {
	for _, mode := range []string{"implement", "design", ""} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			sess, p := w.start(t, mode)
			if !slices.Contains(p.Writable, sess.WorkingDir) {
				t.Errorf("worktree %s not writable: %v", sess.WorkingDir, p.Writable)
			}
			for _, id := range []string{"r2", "r3"} {
				if hasPath(p, w.repos[id]) {
					t.Errorf("%s is visible to a %q session", id, mode)
				}
			}
			if hasPath(p, w.repos["r1"]+"/src") || slices.Contains(p.Readable, w.repos["r1"]) {
				t.Errorf("own checkout is read-only or exposed: %v", p.Readable)
			}
		})
	}

	t.Run("architect writes its worktree and reads every other repository", func(t *testing.T) {
		w := newSandboxWorld(t)
		sess, p := w.start(t, "architect")
		if !slices.Contains(p.Writable, sess.WorkingDir) {
			t.Errorf("worktree not writable: %v", p.Writable)
		}
		for _, id := range []string{"r1", "r2", "r3"} {
			if !slices.Contains(p.Readable, w.repos[id]) {
				t.Errorf("%s not readable: %v", id, p.Readable)
			}
			if slices.Contains(p.Writable, w.repos[id]) {
				t.Errorf("%s writable to an architect", id)
			}
		}
	})

	t.Run("product reads every repository and writes none", func(t *testing.T) {
		w := newSandboxWorld(t)
		_, p := w.start(t, "product")
		for _, id := range []string{"r1", "r2", "r3"} {
			if !slices.Contains(p.Readable, w.repos[id]) {
				t.Errorf("%s not readable: %v", id, p.Readable)
			}
			if hasPath(portsWritable(p), w.repos[id]) {
				t.Errorf("%s writable to a product session", id)
			}
		}
	})
}

func TestSandbox_PolicyAlwaysHoldsTheAgentHomeShellTempDirAndGitCommonDir(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "architect"} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			shells := &fakeShells{dir: t.TempDir()}
			w.svc.Shells = shells
			sess, p := w.start(t, mode)
			for name, want := range map[string]string{
				"agent Claude home":    w.home,
				"virtual shell":        shells.dir,
				"temp dir":             filepath.Clean(os.TempDir()),
				"git common directory": filepath.Join(w.repos["r1"], ".git"),
			} {
				if !slices.Contains(p.Writable, want) {
					t.Errorf("%s %s not writable for %s: %v", name, want, sess.ID, p.Writable)
				}
			}
			if p.WorkingDir != sess.WorkingDir {
				t.Errorf("working dir %q, want %q", p.WorkingDir, sess.WorkingDir)
			}
			parent := filepath.Dir(w.home)
			for _, list := range [][]string{p.Readable, p.Writable} {
				if slices.Contains(list, parent) {
					t.Errorf("agent home's parent %s listed for %s: %v", parent, sess.ID, list)
				}
			}
		})
	}
}

func TestSandbox_TheGitCommonDirIsReadFromTheWorktreeLink(t *testing.T) {
	w := newSandboxWorld(t)
	common := filepath.Join(t.TempDir(), "elsewhere.git")
	w.svc.workspaces.(*fakeProvisioner).onCreate = func(dir string) {
		link := "gitdir: " + filepath.Join(common, "worktrees", "x") + "\n"
		_ = os.WriteFile(filepath.Join(dir, ".git"), []byte(link), 0o644)
		_ = os.MkdirAll(filepath.Join(common, "worktrees", "x"), 0o755)
		_ = os.WriteFile(filepath.Join(common, "worktrees", "x", "commondir"), []byte("../..\n"), 0o644)
	}
	_, p := w.start(t, "implement")
	if !slices.Contains(p.Writable, common) {
		t.Errorf("common dir %s not writable: %v", common, p.Writable)
	}
}

func TestSandbox_ExtraPathsFromSettingsAreReadOnlyOrReadWriteAsConfigured(t *testing.T) {
	w := newSandboxWorld(t)
	w.extra = []ports.SandboxPath{{Path: "/opt/notes"}, {Path: "/opt/scratch", Write: true}}
	_, p := w.start(t, "implement")
	if !slices.Contains(p.Readable, "/opt/notes") || slices.Contains(p.Writable, "/opt/notes") {
		t.Errorf("/opt/notes should be read-only: %+v", p)
	}
	if !slices.Contains(p.Writable, "/opt/scratch") {
		t.Errorf("/opt/scratch should be writable: %+v", p)
	}
}

func TestSandbox_SwitchedOffTheDriverIsNotCalled(t *testing.T) {
	w := newSandboxWorld(t)
	w.off = true
	if _, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if n := len(w.driver.Calls()); n != 0 {
		t.Fatalf("driver called %d times with the sandbox off", n)
	}
	w.off = false // read per session start
	w.start(t, "implement")
}

func TestSandbox_OnWithoutADriverTheStartIsRefused(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Sandbox.Driver = nil
	_, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err == nil || !strings.Contains(err.Error(), "sandbox driver") {
		t.Fatalf("err = %v, want one naming the missing sandbox driver", err)
	}
	prov := w.svc.workspaces.(*fakeProvisioner)
	if len(prov.discarded) != len(prov.created) {
		t.Errorf("the refused start left a worktree behind: created %d, discarded %d", len(prov.created), len(prov.discarded))
	}
}

func TestSandbox_AHeadlessStartIsSandboxedToo(t *testing.T) {
	w := newSandboxWorld(t)
	sess, err := w.svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.svc.Stop(context.Background(), sess.ID) })
	calls := w.driver.Calls()
	if len(calls) != 1 || !slices.Contains(calls[0].Policy.Writable, sess.WorkingDir) {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Command.Dir != sess.WorkingDir {
		t.Errorf("driver got command dir %q, want %q", calls[0].Command.Dir, sess.WorkingDir)
	}
}

func hasPath(p ports.SandboxPolicy, prefix string) bool {
	for _, x := range append(slices.Clone(p.Readable), p.Writable...) {
		if x == prefix || strings.HasPrefix(x, prefix+"/") {
			return true
		}
	}
	return false
}

func portsWritable(p ports.SandboxPolicy) ports.SandboxPolicy {
	return ports.SandboxPolicy{Readable: p.Writable}
}

// fakeShells hands every session the same shell dir.
type fakeShells struct{ dir string }

func (f *fakeShells) Open(context.Context, string, ports.ShellScope) (string, error) {
	return f.dir, nil
}
func (f *fakeShells) Close(string, string) {}

func TestSandboxFacts_EachModeRecordsItsRepositoryVisibilityAndTheDriver(t *testing.T) {
	for mode, want := range map[string]string{
		"implement": domain.SandboxOwnRepo,
		"design":    domain.SandboxOwnRepo,
		"":          domain.SandboxOwnRepo,
		"architect": domain.SandboxAllReposReadOnly,
		"product":   domain.SandboxAllReposReadOnly,
	} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			sess, _ := w.start(t, mode)
			got := w.svc.sessions.Get(sess.ID).Sandbox
			if got == nil || !got.Sandboxed || got.Visibility != want || got.Driver != "recording" {
				t.Errorf("stored facts = %+v, want sandboxed, %q, driver recording", got, want)
			}
			if sess.Sandbox == nil || *sess.Sandbox != *got {
				t.Errorf("returned facts = %+v, stored %+v", sess.Sandbox, got)
			}
		})
	}
}

func TestSandboxFacts_AHeadlessSessionRecordsThemToo(t *testing.T) {
	w := newSandboxWorld(t)
	sess, err := w.svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.svc.Stop(context.Background(), sess.ID) })
	got := w.svc.sessions.Get(sess.ID).Sandbox
	if got == nil || !got.Sandboxed || got.Visibility != domain.SandboxOwnRepo {
		t.Errorf("facts = %+v", got)
	}
}

func TestSandboxFacts_ASessionStartedWithTheSandboxOffRecordsNotSandboxed(t *testing.T) {
	w := newSandboxWorld(t)
	w.off = true
	sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	got := w.svc.sessions.Get(sess.ID).Sandbox
	if got == nil || got.Sandboxed || got.Visibility != domain.SandboxUnrestricted || got.Driver != "" {
		t.Errorf("facts = %+v, want not sandboxed and unrestricted", got)
	}
}

func TestSandboxFacts_ARunningSessionKeepsItsFactsWhenSettingsChange(t *testing.T) {
	w := newSandboxWorld(t)
	sess, _ := w.start(t, "implement")
	w.off = true
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || !got.Sandboxed {
		t.Errorf("facts after the switch = %+v", got)
	}
}

func TestSandboxFacts_AResumeRecordsTheRulesItStartsUnderAndWrapsItsCommand(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	ctx := context.Background()
	sess, _ := w.start(t, "implement")
	if _, err := w.svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}

	w.off = true // the person switches the sandbox off, then resumes
	resumed, spec, err := w.svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Sandboxed || got.Visibility != domain.SandboxUnrestricted {
		t.Errorf("after resuming with the sandbox off: %+v", got)
	}
	if resumed.Sandbox == nil || resumed.Sandbox.Sandboxed || spec.Sandbox != nil {
		t.Errorf("returned %+v, spec policy %v", resumed.Sandbox, spec.Sandbox)
	}

	if _, err := w.svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	w.off = false // and back on
	_, spec, err = w.svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	got := w.svc.sessions.Get(sess.ID).Sandbox
	if got == nil || !got.Sandboxed || got.Visibility != domain.SandboxOwnRepo || got.Driver != "recording" {
		t.Errorf("after resuming with the sandbox on: %+v", got)
	}
	calls := w.driver.Calls()
	if spec.Sandbox == nil || len(calls) != 2 || !slices.Contains(calls[1].Policy.Writable, sess.WorkingDir) {
		t.Errorf("resume policy %v, driver calls %d", spec.Sandbox, len(calls))
	}
}

func TestSandboxFacts_AResumedArchitectSeesEveryRepositoryAgain(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	sess, _ := w.start(t, "architect")
	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"r2", "r3"} {
		if !slices.Contains(spec.Sandbox.Readable, w.repos[id]) {
			t.Errorf("%s not readable on resume: %v", id, spec.Sandbox.Readable)
		}
	}
	if got := w.svc.sessions.Get(sess.ID).Sandbox; got == nil || got.Visibility != domain.SandboxAllReposReadOnly {
		t.Errorf("facts = %+v", got)
	}
}

func TestSandboxFacts_ASessionFromBeforeTheSandboxHasNoneAndResumesFine(t *testing.T) {
	svc, _ := newInteractiveService(t) // no Sandbox step wired
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if sess.Sandbox != nil {
		t.Fatalf("facts without a sandbox step: %+v", sess.Sandbox)
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if got := svc.sessions.Get(sess.ID).Sandbox; got != nil {
		t.Errorf("facts = %+v, want none", got)
	}
}

// readiness is a Ready that refuses every start.
type readiness struct{ err error }

func (r readiness) Check(context.Context) error { return r.err }

func TestReady_ARefusedStartSpawnsNothingAndLeavesNoWorktree(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	ctx := context.Background()
	sess, _ := w.start(t, "implement")
	if _, err := w.svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	prov := w.svc.workspaces.(*fakeProvisioner)
	created, calls := len(prov.created), len(w.driver.Calls())

	refusal := &domain.StructuredError{Code: "AGENT_CREDENTIAL_MISSING", Message: "Agent not started: no agent credential."}
	w.svc.Ready = readiness{err: refusal}

	_, _, err := w.svc.StartInteractive(ctx, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != error(refusal) {
		t.Errorf("StartInteractive err = %v", err)
	}
	if _, err = w.svc.Start(ctx, StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"}); err != error(refusal) {
		t.Errorf("Start err = %v", err)
	}
	if _, _, err = w.svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID}); err != error(refusal) {
		t.Errorf("ResumeInteractive err = %v", err)
	}
	if len(prov.created) != created || len(w.driver.Calls()) != calls {
		t.Errorf("a refused start spawned: worktrees %d -> %d, driver calls %d -> %d", created, len(prov.created), calls, len(w.driver.Calls()))
	}

	w.svc.Ready = readiness{} // what was missing is added: the next start proceeds
	if _, _, err := w.svc.StartInteractive(ctx, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatalf("next start: %v", err)
	}
}

func TestReady_NothingMissingEnforcesNothingWithoutAGate(t *testing.T) {
	w := newSandboxWorld(t)
	if w.svc.Ready != nil {
		t.Fatal("a service built without a gate must not enforce one")
	}
	w.start(t, "implement")
}

// ownTempDir gives each session a temp dir of its own, as the app does: the
// checkouts of these tests live in the shared system temp dir, which would
// otherwise be granted wholesale.
func (w *sandboxWorld) ownTempDir() {
	w.svc.Sandbox.TempDir = func(id string) string { return filepath.Join(os.TempDir(), "harness-session-"+id) }
}

func TestSandbox_HiddenListsEveryRepositoryAndTheWorkspacesRootThatTheModeDoesNotGrant(t *testing.T) {
	for _, mode := range []string{"implement", "design", ""} {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			w.ownTempDir()
			sess, p := w.start(t, mode)
			for _, want := range []string{w.repos["r1"], w.repos["r2"], w.repos["r3"], w.wsRoot} {
				if !slices.Contains(p.Hidden, want) {
					t.Errorf("%s is not hidden from a %q session: %v", want, mode, p.Hidden)
				}
			}
			for _, granted := range []string{sess.WorkingDir, filepath.Join(w.repos["r1"], ".git")} {
				if slices.Contains(p.Hidden, granted) {
					t.Errorf("granted %s is hidden", granted)
				}
			}
		})
	}

	t.Run("architect and product hide none of the repositories they are granted", func(t *testing.T) {
		for _, mode := range []string{"architect", "product"} {
			w := newSandboxWorld(t)
			w.ownTempDir()
			_, p := w.start(t, mode)
			for _, id := range []string{"r1", "r2", "r3"} {
				if slices.Contains(p.Hidden, w.repos[id]) {
					t.Errorf("%s hides granted %s: %v", mode, id, p.Hidden)
				}
			}
			if !slices.Contains(p.Hidden, w.wsRoot) {
				t.Errorf("%s does not hide the workspaces root: %v", mode, p.Hidden)
			}
		}
	})

	t.Run("a one-shot hides every checkout and the workspaces root", func(t *testing.T) {
		w := newSandboxWorld(t)
		w.wsRoot = "/Volumes/ws"
		w.svc.Sandbox.Checkouts = func() []string { return []string{"/opt/a", "/Volumes/x/b"} }
		wrap, err := w.svc.Sandbox.OneShotWrap(nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := wrap(commandIn("/tmp")); err != nil {
			t.Fatal(err)
		}
		p := w.driver.Calls()[0].Policy
		for _, want := range []string{"/opt/a", "/Volumes/x/b", w.wsRoot} {
			if !slices.Contains(p.Hidden, want) {
				t.Errorf("%s not hidden from a one-shot: %v", want, p.Hidden)
			}
		}
	})
}

func TestBuildSandboxPolicy_AHiddenPathUnderAGrantedOneIsNotHidden(t *testing.T) {
	p := BuildSandboxPolicy(SandboxSession{
		Mode: domain.SessionModeImplement, Worktree: "/ws/r/wt", GitCommonDir: "/opt/r/.git",
		Repositories: []string{"/opt/r", "/ws/r/wt/vendored", "/opt/other"}, WorkspacesRoot: "/ws",
	})
	want := []string{"/opt/r", "/opt/other", "/ws"}
	if !slices.Equal(p.Hidden, want) {
		t.Errorf("Hidden = %v, want %v", p.Hidden, want)
	}
}

// linkWorktree makes the session's worktree a linked one of a common git dir
// with the given sibling entries, answering the common dir.
func (w *sandboxWorld) linkWorktree(siblings ...string) string {
	common := filepath.Join(filepath.Dir(w.wsRoot), filepath.Base(w.wsRoot)+"-common.git")
	w.svc.workspaces.(*fakeProvisioner).onCreate = func(dir string) {
		_ = os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+filepath.Join(common, "worktrees", "own")+"\n"), 0o644)
		for _, n := range append([]string{"own"}, siblings...) {
			_ = os.MkdirAll(filepath.Join(common, "worktrees", n), 0o755)
		}
		_ = os.WriteFile(filepath.Join(common, "worktrees", "own", "commondir"), []byte("../..\n"), 0o644)
	}
	return common
}

func TestSandbox_OtherWorktreesAdminEntriesAreReadOnlyAndOnlyTheOwnEntryIsWritable(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "architect"} {
		for name, siblings := range map[string][]string{"with other worktrees": {"a", "b"}, "alone": nil} {
			t.Run("mode "+mode+" "+name, func(t *testing.T) {
				w := newSandboxWorld(t)
				common := w.linkWorktree(siblings...)
				_, p := w.start(t, mode)
				if !slices.Contains(p.Writable, common) {
					t.Errorf("common dir not writable: %v", p.Writable)
				}
				if !slices.Equal(p.CreateOnly, []string{filepath.Join(common, "worktrees")}) {
					t.Errorf("worktrees dir not create-only: %v", p.CreateOnly)
				}
				own := filepath.Join(common, "worktrees", "own")
				if !slices.Contains(p.Writable, own) {
					t.Errorf("own entry not writable: %v", p.Writable)
				}
				if slices.Contains(p.Writable, filepath.Join(common, "worktrees")) {
					t.Errorf("worktrees dir writable as a whole: %v", p.Writable)
				}
				for _, s := range siblings {
					other := filepath.Join(common, "worktrees", s)
					if slices.Contains(p.Writable, other) || !slices.Contains(p.Readable, other) {
						t.Errorf("other entry %s not read-only: readable %v writable %v", s, p.Readable, p.Writable)
					}
				}
			})
		}
	}
}

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
		Driver:  w.driver,
		Enabled: func() bool { return !w.off },
		Home:    w.home,
		Extra:   func() []ports.SandboxPath { return w.extra },
	}
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

package orchestration

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestSandbox_AnArchitectReadsTheRepositorysWorktreesFolderAndWritesItsOwnWorktree(t *testing.T) {
	w := newSandboxWorld(t)
	w.ownTempDir()
	sess, p := w.start(t, "architect")
	folder := filepath.Join(w.wsRoot, domain.RepositoryWorktreesDirName(w.svc.catalog.Repositories.(*fakeResolver).repo))
	if !slices.Contains(p.Readable, folder) || slices.Contains(p.Writable, folder) {
		t.Errorf("worktrees folder %s: readable %v writable %v", folder, p.Readable, p.Writable)
	}
	if !slices.Contains(p.Writable, sess.WorkingDir) {
		t.Errorf("own worktree %s not writable: %v", sess.WorkingDir, p.Writable)
	}
	if !slices.Contains(p.Hidden, w.wsRoot) {
		t.Errorf("the workspaces root is no longer hidden: %v", p.Hidden)
	}
}

func TestSandbox_NoOtherModeGrantsTheWorktreesFolder(t *testing.T) {
	for _, mode := range []string{"implement", "design", "", "product"} {
		for _, readAll := range []bool{false, true} {
			w := newSandboxWorld(t)
			w.ownTempDir()
			_, p := w.startWithReadAll(t, mode, readAll)
			folder := filepath.Join(w.wsRoot, domain.RepositoryWorktreesDirName(w.svc.catalog.Repositories.(*fakeResolver).repo))
			if slices.Contains(p.Readable, folder) || slices.Contains(p.Writable, folder) {
				t.Errorf("mode %q (read all %v) is granted %s", mode, readAll, folder)
			}
		}
	}
}

func TestBuildSandboxPolicy_AnArchitectsWorktreesFolderIsReadOnlyAroundItsOwnWorktree(t *testing.T) {
	p := BuildSandboxPolicy(SandboxSession{
		Mode: domain.SessionModeArchitect, Worktree: "/ws/r-1/wt", WorktreesDir: "/ws/r-1",
		Repositories: []string{"/opt/r"}, WorkspacesRoot: "/ws",
	})
	if !slices.Contains(p.Readable, "/ws/r-1") || !slices.Contains(p.Writable, "/ws/r-1/wt") || slices.Contains(p.Writable, "/ws/r-1") {
		t.Errorf("readable %v writable %v", p.Readable, p.Writable)
	}
	if !slices.Equal(p.Hidden, []string{"/ws"}) {
		t.Errorf("hidden = %v", p.Hidden)
	}
}

// startSpec starts an interactive session of mode in the sandbox world, with the
// server able to hand out shims and the lookup's URL, and answers its spec.
func (w *sandboxWorld) startSpec(t *testing.T, mode string) ports.AgentSpec {
	t.Helper()
	w.ownTempDir()
	w.svc.ShimBinary = filepath.Join(t.TempDir(), "harness")
	w.svc.SessionBranchGateURL = func(id string) string { return "http://127.0.0.1:1/api/sessions/" + id + "/branch_gate" }
	w.svc.SessionTaskWorktreesURL = func(id string) string { return "http://127.0.0.1:1/api/sessions/" + id + "/task_worktrees" }
	req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode, TicketID: "tk1", RunsOn: domain.RunnerTUI}
	sess, spec, err := w.svc.StartInteractive(context.Background(), req)
	if err != nil {
		t.Fatalf("start %q: %v", mode, err)
	}
	if sess.ID == "" {
		t.Fatal("no session")
	}
	return spec
}

func worktreeReadHooks(spec ports.AgentSpec) []ports.HookCommand {
	var found []ports.HookCommand
	for _, group := range spec.Hooks["PreToolUse"] {
		for _, h := range group.Hooks {
			if strings.HasPrefix(h.Command, ports.WorktreeReadHookCommand) {
				if group.Matcher != "Read|Grep|Glob|LS|Bash|Edit|Write|MultiEdit" {
					panic("the hook's matcher is " + group.Matcher)
				}
				found = append(found, h)
			}
		}
	}
	return found
}

func TestWorktreeReadHook_AnArchitectSessionGetsTheHookAndTheFolderAndTheLookupInItsEnvironment(t *testing.T) {
	w := newSandboxWorld(t)
	spec := w.startSpec(t, "architect")
	if n := len(worktreeReadHooks(spec)); n != 1 {
		t.Fatalf("the architect has %d worktree read hooks, want 1", n)
	}
	folder := filepath.Join(w.wsRoot, domain.RepositoryWorktreesDirName(w.svc.catalog.Repositories.(*fakeResolver).repo))
	env := strings.Join(spec.Env, "\n")
	for _, want := range []string{ports.WorktreesRootEnv + "=" + folder, ports.TaskWorktreesURLEnv + "=http://127.0.0.1:1/api/sessions/"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s:\n%s", want, env)
		}
	}
	if !strings.Contains(worktreeReadHooks(spec)[0].Command, "the harness could not be reached; try again") {
		t.Errorf("the hook has no fail-closed fallback: %q", worktreeReadHooks(spec)[0].Command)
	}
}

func TestWorktreeReadHook_NoOtherModeAndNoUnsandboxedSessionGetsIt(t *testing.T) {
	for _, mode := range []string{"implement", "design", "product"} {
		w := newSandboxWorld(t)
		spec := w.startSpec(t, mode)
		if n := len(worktreeReadHooks(spec)); n != 0 || strings.Contains(strings.Join(spec.Env, "\n"), ports.WorktreesRootEnv) {
			t.Errorf("a %s session got the worktree read hook (%d) or its environment", mode, n)
		}
	}
	w := newSandboxWorld(t)
	w.off = true
	if spec := w.startSpec(t, "architect"); len(worktreeReadHooks(spec)) != 0 {
		t.Error("an unsandboxed architect has nothing to narrow, so no hook")
	}
}

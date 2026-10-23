package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"github.com/rfbatista/llmkit/claude/mcpapprove"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// isolate runs the world's sessions on a claude manager built as the app
// builds it: the agent home as its config dir, isolated.
func (w *sandboxWorld) isolate(t *testing.T) {
	t.Helper()
	exe, _ := os.Executable()
	mgr := claude.New(llmkit.Options{
		Bin: exe, ConfigDir: w.home, Isolated: true,
		ApprovalURL: func(id string) string {
			return "http://127.0.0.1:8080" + mcpapprove.PathPrefix + id
		},
		ApprovalTimeout: time.Hour,
	})
	w.svc.runtime = mgr
	w.svc.broker = mgr.Approvals()
}

func (w *sandboxWorld) startHeadless(t *testing.T) (*domain.Session, error) {
	t.Helper()
	sess, err := w.svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err == nil {
		t.Cleanup(func() { _ = w.svc.Stop(context.Background(), sess.ID) })
	}
	return sess, err
}

func TestSandboxWire_TheFinalCommandRunsInTheAgentHomeAndIgnoresThePersonsClaude(t *testing.T) {
	w := newSandboxWorld(t)
	w.isolate(t)
	if _, err := w.startHeadless(t); err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	cmd := calls[0].Command
	for _, want := range []string{"CLAUDE_CONFIG_DIR=" + w.home, "DISABLE_AUTOUPDATER=1"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("env lacks %q", want)
		}
	}
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--setting-sources project", "--strict-mcp-config"} {
		if !strings.Contains(args, want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		mine := filepath.Join(home, ".claude")
		for _, e := range cmd.Env {
			if strings.Contains(e, mine) {
				t.Errorf("env %q reaches into the person's %s", e, mine)
			}
		}
	}
}

func TestSandboxWire_ThePolicyHoldsEveryFileLlmkitMadeForTheProcess(t *testing.T) {
	w := newSandboxWorld(t)
	files := []string{filepath.Join(t.TempDir(), "mcp.json"), filepath.Join(t.TempDir(), "instructions")}
	sess := SandboxSession{Mode: domain.SessionModeDefault, Worktree: t.TempDir(), AgentHome: w.home}
	wrap := sandboxWrap(w.driver, sess)
	if _, err := wrap(llmkit.Command{Path: "claude", Dir: sess.Worktree, Files: files}); err != nil {
		t.Fatal(err)
	}
	policy := w.driver.Calls()[0].Policy
	for _, f := range files {
		if !slices.Contains(policy.Readable, f) {
			t.Errorf("policy readable = %v, lacks %s", policy.Readable, f)
		}
	}
}

func TestSandboxWire_AHeadlessStartsFilesReachThePolicy(t *testing.T) {
	w := newSandboxWorld(t)
	w.isolate(t)
	if _, err := w.startHeadless(t); err != nil {
		t.Fatal(err)
	}
	call := w.driver.Calls()[0]
	var found int
	for i, a := range call.Command.Args {
		if (a == "--mcp-config" || a == "--settings") && i+1 < len(call.Command.Args) {
			if p := call.Command.Args[i+1]; filepath.IsAbs(p) {
				found++
				if !slices.Contains(call.Policy.Readable, p) {
					t.Errorf("%s %s is not in the policy: %v", a, p, call.Policy.Readable)
				}
			}
		}
	}
	if found == 0 {
		t.Skip("this start passes no file to claude")
	}
}

func TestSandboxWire_ADriverWrapErrorFailsTheHeadlessStartCleanly(t *testing.T) {
	w := newSandboxWorld(t)
	w.isolate(t)
	w.driver.Err = errors.New("no confinement today")
	_, err := w.startHeadless(t)
	if err == nil || !strings.Contains(err.Error(), "no confinement today") {
		t.Fatalf("err = %v, want the driver's", err)
	}
	prov := w.svc.workspaces.(*fakeProvisioner)
	if len(prov.discarded) != len(prov.created) {
		t.Errorf("the failed start left a worktree behind: created %d, discarded %d", len(prov.created), len(prov.discarded))
	}
	if n := len(w.svc.sessions.List(context.Background(), ports.SessionFilter{})); n != 0 {
		t.Errorf("%d sessions recorded for a failed start", n)
	}
}

func TestSandboxWire_ASessionsOwnGitAndShimPathsAreAllowed(t *testing.T) {
	got := sessionAllow([]string{
		ports.ShimDirEnv + "=/tmp/shims",
		"GIT_CONFIG_GLOBAL=/data/git/sessions/s1/gitconfig",
		"GIT_CONFIG_VALUE_1=cache --socket '/tmp/harness-1/ab-cd/cred.sock'",
		"GIT_SSH_COMMAND=ssh -o 'UserKnownHostsFile=/data/git/known_hosts' -o X=y",
	})
	want := []ports.SandboxPath{
		{Path: "/tmp/shims"},
		{Path: "/data/git/sessions/s1", Write: true},
		{Path: "/tmp/harness-1/ab-cd", Write: true},
		{Path: "/data/git/known_hosts", Write: true},
	}
	for _, x := range want {
		if !slices.Contains(got, x) {
			t.Errorf("sessionAllow lacks %+v: %+v", x, got)
		}
	}
}

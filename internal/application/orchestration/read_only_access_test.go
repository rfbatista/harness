package orchestration

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Read-only access to a shared worktree (Glossary: Access): preset by the
// mode, a read-only sandbox grant, and a brief that says so.

func joinAs(t *testing.T, svc *Service, creator *domain.Session, mode, access string) (*domain.Session, ports.AgentSpec) {
	t.Helper()
	sess, spec, err := svc.StartInteractive(context.Background(), InteractiveRequest{
		ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: mode, Access: access,
		RunsOn: domain.RunnerTUI, WorktreeID: creator.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("join as %s/%q: %v", mode, access, err)
	}
	return sess, spec
}

func TestTheAccessOfAJoinedSessionIsPresetByItsModeAndAGivenAccessWins(t *testing.T) {
	svc, _, creator := joinWorld(t)
	for _, c := range []struct {
		mode, access string
		want         domain.SessionAccess
	}{
		{"review", "", domain.AccessReadOnly},
		{"qa", "", domain.AccessReadOnly},
		{"implement", "", domain.AccessWrite},
		{"test", "", domain.AccessWrite},
		{"operate", "", domain.AccessWrite},
		{"review", "write", domain.AccessWrite},
		{"implement", "read_only", domain.AccessReadOnly},
	} {
		sess, _ := joinAs(t, svc, creator, c.mode, c.access)
		if sess.Access != c.want {
			t.Errorf("%s with access %q recorded %q, want %q", c.mode, c.access, sess.Access, c.want)
		}
		if stored := svc.sessions.Get(sess.ID); stored.Access != c.want {
			t.Errorf("%s stored access %q, want %q", c.mode, stored.Access, c.want)
		}
	}
}

func TestAReadOnlySessionsSandboxGrantsTheWorktreeAndTheGitDirReadOnly(t *testing.T) {
	for _, mode := range []domain.SessionMode{domain.SessionModeImplement, domain.SessionModeReview} {
		p := BuildSandboxPolicy(SandboxSession{
			Mode: mode, Access: domain.AccessReadOnly, Worktree: "/w", GitCommonDir: "/g", OwnGitDir: "/g/worktrees/w",
			TempDir: "/t", CommitCheckDir: "/hooks/ws1",
		})
		for _, path := range []string{"/w", "/g", "/g/worktrees/w"} {
			if slices.Contains(p.Writable, path) {
				t.Errorf("%s: %s is writable: %v", mode, path, p.Writable)
			}
		}
		if !slices.Contains(p.Readable, "/w") || !slices.Contains(p.Readable, "/g") {
			t.Errorf("%s: worktree and git dir not readable: %v", mode, p.Readable)
		}
		if len(p.CreateOnly) != 0 {
			t.Errorf("%s: create-only %v; a read-only session adds no worktrees", mode, p.CreateOnly)
		}
		if !slices.Contains(p.Writable, "/t") {
			t.Errorf("%s: the temp dir is not writable, so builds and tests could not run: %v", mode, p.Writable)
		}
		if !slices.Contains(p.Readable, "/hooks/ws1") || slices.Contains(p.Writable, "/hooks/ws1") {
			t.Errorf("%s: the hooks dir is not read-only: %v / %v", mode, p.Readable, p.Writable)
		}
	}
	w := BuildSandboxPolicy(SandboxSession{Mode: domain.SessionModeImplement, Access: domain.AccessWrite, Worktree: "/w", GitCommonDir: "/g"})
	if !slices.Contains(w.Writable, "/w") || !slices.Contains(w.Writable, "/g") {
		t.Errorf("a write session lost its grants: %v", w.Writable)
	}
}

func TestAJoinedReadOnlySessionRunsWithoutOptionalGitLocksAndIsToldItIsReadOnly(t *testing.T) {
	svc, _, creator := joinWorld(t)
	reader, spec := joinAs(t, svc, creator, "review", "")
	if !slices.Contains(spec.Env, "GIT_OPTIONAL_LOCKS=0") {
		t.Errorf("env %v lacks GIT_OPTIONAL_LOCKS=0, so git status would try to refresh a read-only index", spec.Env)
	}
	if !strings.Contains(spec.AppendSystem, "read-only access") || !strings.Contains(spec.AppendSystem, "Operation not permitted") ||
		!strings.Contains(spec.AppendSystem, reader.Branch) {
		t.Errorf("the brief does not say plainly that the session is read-only on %s:\n%s", reader.Branch, spec.AppendSystem)
	}

	_, writer := joinAs(t, svc, creator, "test", "")
	if slices.Contains(writer.Env, "GIT_OPTIONAL_LOCKS=0") || strings.Contains(writer.AppendSystem, "read-only access") {
		t.Error("a write session got the read-only environment or brief")
	}
}

func TestAResumeOfAReadOnlySessionRebuildsTheSameReadOnlyPolicyAndEnvironment(t *testing.T) {
	w := newSandboxWorld(t)
	creator, _ := w.start(t, "implement")
	reader, _ := joinAs(t, w.svc, creator, "review", "")
	if err := w.svc.sessions.UpdateStatus(reader.ID, domain.SessionStopped); err != nil {
		t.Fatal(err)
	}
	w.svc.Transcripts = fakeTranscripts{}

	_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: reader.ID, RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	last := calls[len(calls)-1].Policy
	if slices.Contains(last.Writable, creator.WorkingDir) {
		t.Errorf("a resumed read-only session may write its worktree: %v", last.Writable)
	}
	if !slices.Contains(spec.Env, "GIT_OPTIONAL_LOCKS=0") || !strings.Contains(spec.AppendSystem, "read-only access") {
		t.Error("a resumed read-only session lost its environment or brief")
	}
}

func TestASessionThatCutsItsOwnWorktreeWritesItWhateverItsModesPreset(t *testing.T) {
	svc, _ := newInteractiveService(t)
	for _, mode := range []string{"review", "qa"} {
		sess, err := startOn(t, svc, InteractiveRequest{Mode: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if sess.Access != domain.AccessWrite || sess.Joined {
			t.Errorf("%s cut its own worktree and recorded access %q joined %v", mode, sess.Access, sess.Joined)
		}
	}
	if _, err := startOn(t, svc, InteractiveRequest{Mode: "review", Access: "read_only"}); err == nil {
		t.Error("read-only was accepted with no worktree to join")
	}
}

func TestTheArchitectureHookFiresBeforeNotebookEditToo(t *testing.T) {
	groups := ArchitectureHooks("http://harness.test/api/sessions/s1/architecture")[claude.HookPreToolUse]
	if len(groups) != 1 || !strings.Contains(groups[0].Matcher, "NotebookEdit") {
		t.Errorf("matcher %+v lacks NotebookEdit, so a read-only session's notebook write is refused only by the OS", groups)
	}
}

func TestReadOnlyAccessIsRefusedWhileTheAgentSandboxIsOff(t *testing.T) {
	svc, _, creator := joinWorld(t)
	svc.Sandbox = &Sandbox{Enabled: func() bool { return false }}
	sessions := len(svc.sessions.List(context.Background(), ports.SessionFilter{}))
	const sentence = "Read-only access needs the agent sandbox; it is off for this session."

	for name, req := range map[string]InteractiveRequest{
		"an explicit read_only":                    {Mode: "implement", Access: "read_only"},
		"the Reviewer's default":                   {Mode: "review"},
		"the QA default":                           {Mode: "qa"},
		"an explicit read_only on a write default": {Mode: "test", Access: "read_only"},
	} {
		req.WorktreeID = creator.WorkspaceID
		_, err := startOn(t, svc, req)
		if errs.Code(err) != "ACCESS_INVALID" || err == nil || !strings.Contains(err.Error(), sentence) {
			t.Errorf("%s: %v, want ACCESS_INVALID saying %q", name, err, sentence)
		}
	}
	if got := len(svc.sessions.List(context.Background(), ports.SessionFilter{})); got != sessions {
		t.Errorf("a refusal recorded sessions: %d → %d", sessions, got)
	}
	for _, req := range []InteractiveRequest{{Mode: "review", Access: "write"}, {Mode: "implement"}} {
		req.WorktreeID = creator.WorkspaceID
		if sess, err := startOn(t, svc, req); err != nil || sess.Access != domain.AccessWrite {
			t.Errorf("%+v: %v %v; write access needs no sandbox", req, sess, err)
		}
	}
}

func TestAResumeOfAReadOnlySessionIsRefusedWhileTheSandboxIsOffAsAStartIs(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Transcripts = fakeTranscripts{}
	creator, _ := w.start(t, "implement")
	reader, _ := joinAs(t, w.svc, creator, "review", "")
	writer, _ := joinAs(t, w.svc, creator, "test", "")
	for _, id := range []string{reader.ID, writer.ID} {
		if err := w.svc.sessions.UpdateStatus(id, domain.SessionStopped); err != nil {
			t.Fatal(err)
		}
	}
	w.off = true

	_, _, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: reader.ID, RunsOn: domain.RunnerTUI})
	if errs.Code(err) != "ACCESS_INVALID" || err == nil || !strings.Contains(err.Error(), "Read-only access needs the agent sandbox; it is off for this session.") {
		t.Fatalf("resume of a read-only session with the sandbox off: %v, want the start's refusal", err)
	}
	if got := w.svc.sessions.Get(reader.ID); got.Status != domain.SessionStopped {
		t.Errorf("a refused resume moved the session to %s", got.Status)
	}
	if got, _ := w.svc.Get(context.Background(), reader.ID); got == nil || got.Resumable || got.ResumeBlocked != "ACCESS_INVALID" {
		t.Errorf("the session reads as resumable (%+v), want it blocked with ACCESS_INVALID", got)
	}
	if _, _, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: writer.ID, RunsOn: domain.RunnerTUI}); err != nil {
		t.Errorf("a write session cannot resume with the sandbox off: %v", err)
	}
	w.off = false
	if _, _, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: reader.ID, RunsOn: domain.RunnerTUI}); err != nil {
		t.Errorf("the read-only session cannot resume once the sandbox is back on: %v", err)
	}
}

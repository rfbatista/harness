package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agents/claudecli"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const testHookBase = "http://127.0.0.1:8080/api/interactive_session_started?session_id="

type fakeTranscripts struct{ err error }

func (f fakeTranscripts) CanResume(dir, id string) error { return f.err }

func newInteractiveService(t *testing.T) (*Service, *fakeProvisioner) {
	t.Helper()
	svc, _, prov := newTestService(t)
	svc.SessionHookURL = func(id string) string { return testHookBase + id }
	svc.Transcripts = fakeTranscripts{}
	return svc, prov
}

// launched is what a terminal host would run for a spec: the claude command
// the claudecli agent builds from it, so these tests keep checking the flags a
// session actually gets.
type launched struct {
	SessionID string
	Dir       string
	Args      []string
	Spec      ports.AgentSpec
}

func launchOf(t *testing.T, spec ports.AgentSpec) launched {
	t.Helper()
	cmd, err := claudecli.New("").Command(spec)
	if err != nil {
		t.Fatalf("claude command for %+v: %v", spec, err)
	}
	return launched{SessionID: spec.SessionID, Dir: cmd.Dir, Args: cmd.Args, Spec: spec}
}

func startInteractive(t *testing.T, svc *Service, req InteractiveRequest) (*domain.Session, launched) {
	t.Helper()
	if req.ProjectID == "" {
		req.ProjectID, req.RepositoryID, req.TicketID = "p1", "r1", "tk1"
	}
	sess, spec, err := svc.StartInteractive(context.Background(), req)
	if err != nil {
		t.Fatalf("StartInteractive: %v", err)
	}
	return sess, launchOf(t, spec)
}

// flag returns the value after the first occurrence of name in args.
func flag(args []string, name string) (string, bool) {
	i := slices.Index(args, name)
	if i < 0 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func codeOf(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func TestStartInteractive_ProvisionsAndRecords(t *testing.T) {
	svc, prov := newInteractiveService(t)
	sess, launch := startInteractive(t, svc, InteractiveRequest{})

	if len(prov.created) != 1 {
		t.Fatalf("provisioned %d workspaces, want 1", len(prov.created))
	}
	ws := prov.created[0]
	if !sess.Interactive || sess.TicketID != "tk1" || sess.Status != domain.SessionRunning {
		t.Fatalf("session not recorded as a running interactive session on tk1: %+v", sess)
	}
	if sess.ClaudeSessionID != sess.ID || launch.SessionID != sess.ID {
		t.Fatalf("claude session id %q / launch %q, want the session id %q", sess.ClaudeSessionID, launch.SessionID, sess.ID)
	}
	if sess.Task != "Ship the thing" {
		t.Fatalf("task = %q, want the ticket title", sess.Task)
	}
	if want := "agent/ship-the-thing-" + sess.ID[:8]; sess.Branch != want {
		t.Fatalf("branch = %q, want %q", sess.Branch, want)
	}
	if launch.Dir != ws.Path || sess.WorkingDir != ws.Path {
		t.Fatalf("launch dir %q / working dir %q, want the worktree %q", launch.Dir, sess.WorkingDir, ws.Path)
	}
	if got := sessionOf(svc, sess.ID); got == nil || !got.Interactive {
		t.Fatalf("not persisted: %+v", got)
	}
}

func TestStartInteractive_TwoSessionsOnOneTaskGetDistinctBranches(t *testing.T) {
	svc, _ := newInteractiveService(t)
	a, _ := startInteractive(t, svc, InteractiveRequest{})
	b, _ := startInteractive(t, svc, InteractiveRequest{})
	if a.Branch == b.Branch {
		t.Fatalf("both sessions on branch %q", a.Branch)
	}
}

func TestStartInteractive_ArgsCarryTaskContext(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, launch := startInteractive(t, svc, InteractiveRequest{Prompt: "--say hi", AutoAccept: "edits"})
	args := launch.Args

	if v, _ := flag(args, "--session-id"); v != sess.ID {
		t.Errorf("--session-id = %q, want %q", v, sess.ID)
	}
	if v, _ := flag(args, "--append-system-prompt"); !strings.Contains(v, "Ship the thing") {
		t.Errorf("task brief missing from --append-system-prompt: %q", v)
	}
	mcp, ok := flag(args, "--mcp-config")
	if !ok {
		t.Fatalf("no --mcp-config in %q", args)
	}
	var cfg struct {
		MCPServers map[string]struct{ Type, URL string } `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(mcp), &cfg); err != nil {
		t.Fatalf("--mcp-config is not JSON: %v", err)
	}
	if task := cfg.MCPServers["task"]; task.URL != testTaskServerBase+sess.ID || task.Type != "http" {
		t.Errorf("task server = %+v, want http %s", task, testTaskServerBase+sess.ID)
	}
	if _, ok := cfg.MCPServers["approval"]; ok {
		t.Error("interactive session got the approval server; the terminal asks instead")
	}
	if v, _ := flag(args, "--allowedTools"); !strings.Contains(v, "mcp__task__get_task") || strings.Contains(v, " ") {
		t.Errorf("--allowedTools = %q, want one comma-joined value with the task tools", v)
	}
	if v, _ := flag(args, "--permission-mode"); v != "acceptEdits" {
		t.Errorf("--permission-mode = %q, want acceptEdits", v)
	}
	settings, _ := flag(args, "--settings")
	if !strings.Contains(settings, "SessionStart") || !strings.Contains(settings, testHookBase+sess.ID) {
		t.Errorf("--settings lacks the SessionStart hook for this session: %s", settings)
	}
	// The prompt is last, behind --, so a prompt that looks like a flag or a
	// variadic flag before it cannot swallow it.
	if n := len(args); n < 2 || args[n-2] != "--" || args[n-1] != "--say hi" {
		t.Errorf("args do not end with -- and the prompt: %q", args[max(0, n-3):])
	}
	for _, headless := range []string{"--print", "--output-format", "--input-format", "--permission-prompt-tool"} {
		if slices.Contains(args, headless) {
			t.Errorf("interactive args contain headless flag %s", headless)
		}
	}
}

func TestStartInteractive_NoPromptOpensIdle(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	if slices.Contains(launch.Args, "--") {
		t.Errorf("args without a prompt contain --: %q", launch.Args)
	}
}

func TestStartInteractive_AgentSkillsBecomePluginDir(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.catalog.Agents.(*fakeResolver).agents = map[string]*domain.Agent{"a1": {
		ID: "a1",
		Skills: []domain.Skill{{
			Name:  "Review",
			Files: []domain.SkillFile{{Path: domain.SkillFileName, Content: domain.SkillMarkdownFor("review", "reviews", "Review.")}},
		}},
	}}
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "a1"})
	if _, ok := flag(launch.Args, "--plugin-dir"); !ok {
		t.Fatalf("no --plugin-dir for an agent with skills: %q", launch.Args)
	}
	if sess.AgentID != "a1" {
		t.Errorf("agent not recorded: %+v", sess)
	}
	// The plugin dir lives until the session ends.
	svc.mu.Lock()
	_, owned := svc.cleanups[sess.ID]
	svc.mu.Unlock()
	if !owned {
		t.Fatal("skill cleanup not registered for the session")
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	_, owned = svc.cleanups[sess.ID]
	svc.mu.Unlock()
	if owned {
		t.Fatal("EndInteractive did not run the skill cleanup")
	}
}

func TestStartInteractive_Validation(t *testing.T) {
	cases := map[string]struct {
		req  InteractiveRequest
		code string
	}{
		"no ticket":       {InteractiveRequest{ProjectID: "p1", RepositoryID: "r1"}, "INVALID_INPUT"},
		"unknown ticket":  {InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "nope"}, "TICKET_NOT_FOUND"},
		"foreign ticket":  {InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk2"}, "CROSS_PROJECT_ACCESS"},
		"no repository":   {InteractiveRequest{ProjectID: "p1", TicketID: "tk1"}, "INVALID_INPUT"},
		"unknown agent":   {InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", AgentID: "x"}, "AGENT_NOT_FOUND"},
		"unknown project": {InteractiveRequest{ProjectID: "nope", RepositoryID: "r1", TicketID: "tk1"}, "PROJECT_NOT_FOUND"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, prov := newInteractiveService(t)
			_, _, err := svc.StartInteractive(context.Background(), tc.req)
			if codeOf(err) != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
			if len(prov.created) != 0 {
				t.Fatalf("a rejected request provisioned a worktree: %v", prov.created)
			}
		})
	}
}

func TestEndInteractive_StatusFromHowItEnded(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		closed bool
		want   domain.SessionStatus
	}{
		{"clean exit", 0, false, domain.SessionDone},
		{"crash", 2, false, domain.SessionFailed},
		{"closed by user", 143, true, domain.SessionStopped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newInteractiveService(t)
			sess, _ := startInteractive(t, svc, InteractiveRequest{})
			got, err := svc.EndInteractive(context.Background(), sess.ID, tc.code, tc.closed)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("status = %s, want %s", got.Status, tc.want)
			}
			// Idempotent: a second report does not overwrite the first.
			again, err := svc.EndInteractive(context.Background(), sess.ID, 1, false)
			if err != nil || again.Status != tc.want {
				t.Fatalf("second end = %+v, %v; want status kept at %s", again, err, tc.want)
			}
		})
	}
}

func TestEndInteractive_RejectsHeadlessSessions(t *testing.T) {
	svc, _ := newInteractiveService(t)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	if _, err := svc.EndInteractive(context.Background(), d.ID, 0, false); codeOf(err) != "SESSION_NOT_INTERACTIVE" {
		t.Fatalf("err = %v, want SESSION_NOT_INTERACTIVE", err)
	}
	if _, err := svc.EndInteractive(context.Background(), "nope", 0, false); codeOf(err) != "SESSION_NOT_FOUND" {
		t.Fatalf("err = %v, want SESSION_NOT_FOUND", err)
	}
}

func TestResumeInteractive_ResumesTheLatestConversation(t *testing.T) {
	svc, prov := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})

	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); codeOf(err) != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("resume of a running session = %v, want SESSION_ALREADY_RUNNING", err)
	}

	// /clear moved the conversation; the hook reported it.
	if err := svc.RecordClaudeSession(context.Background(), sess.ID, "after-clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}

	got, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	launch := launchOf(t, spec)
	if v, _ := flag(launch.Args, "--resume"); v != "after-clear" {
		t.Errorf("--resume = %q, want after-clear", v)
	}
	if slices.Contains(launch.Args, "--session-id") {
		t.Errorf("resume args also set --session-id: %q", launch.Args)
	}
	if launch.Dir != sess.WorkingDir {
		t.Errorf("resume dir = %q, want the session's worktree %q", launch.Dir, sess.WorkingDir)
	}
	if v, _ := flag(launch.Args, "--append-system-prompt"); !strings.Contains(v, "Ship the thing") {
		t.Errorf("resumed session lost the task brief: %q", v)
	}
	if got.Status != domain.SessionRunning {
		t.Errorf("status after resume = %s, want running", got.Status)
	}
	if len(prov.created) != 1 {
		t.Errorf("resume provisioned another worktree: %v", prov.created)
	}
}

func TestResumeInteractive_MissingTranscript(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, true); err != nil {
		t.Fatal(err)
	}
	svc.Transcripts = fakeTranscripts{err: &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "gone"}}
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); codeOf(err) != "SESSION_TRANSCRIPT_MISSING" {
		t.Fatalf("err = %v, want SESSION_TRANSCRIPT_MISSING", err)
	}
	if got := sessionOf(svc, sess.ID); got.Status != domain.SessionStopped {
		t.Fatalf("a failed resume changed the status to %s", got.Status)
	}
}

func TestRecordClaudeSession(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if err := svc.RecordClaudeSession(context.Background(), sess.ID, "c2"); err != nil {
		t.Fatal(err)
	}
	if got := sessionOf(svc, sess.ID); got.ClaudeSessionID != "c2" {
		t.Fatalf("claude session id = %q, want c2", got.ClaudeSessionID)
	}
	if err := svc.RecordClaudeSession(context.Background(), sess.ID, ""); codeOf(err) != "INVALID_INPUT" {
		t.Fatalf("empty id = %v, want INVALID_INPUT", err)
	}
	if err := svc.RecordClaudeSession(context.Background(), "nope", "c3"); codeOf(err) != "SESSION_NOT_FOUND" {
		t.Fatalf("unknown session = %v, want SESSION_NOT_FOUND", err)
	}
}

// The server does not own an interactive session's CLI, so everything that
// would drive it says so instead of failing obscurely.
func TestInteractiveSessionRejectsHeadlessOperations(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	ctx := context.Background()
	// Stop is for the sessions the server runs; the client stops its own.
	if err := svc.Stop(ctx, sess.ID); codeOf(err) != "SESSION_RUNS_ON_TUI" {
		t.Errorf("stop = %v, want SESSION_RUNS_ON_TUI", err)
	}
	ops := map[string]error{
		"send":     svc.Send(ctx, sess.ID, "hi"),
		"resolve":  svc.Resolve(ctx, sess.ID, "r1", true, ""),
		"answer":   svc.Answer(ctx, sess.ID, "r1", map[string]string{"q": "a"}, nil),
		"auto-run": svc.SetAutoRun(ctx, sess.ID, true),
	}
	for name, err := range ops {
		if codeOf(err) != "SESSION_INTERACTIVE" {
			t.Errorf("%s = %v, want SESSION_INTERACTIVE", name, err)
		}
	}
	if err := svc.Delete(ctx, sess.ID); err != nil {
		t.Errorf("delete = %v, want it to work", err)
	}
}

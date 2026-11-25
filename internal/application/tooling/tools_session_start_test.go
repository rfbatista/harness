package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeStarter records what it was asked to start and records the session, as
// orchestration would.
type fakeStarter struct {
	sessions  ports.SessionRepository
	got       []ports.InteractiveRequest
	err       error // refuses every start
	resumed   []ports.ResumeRequest
	resumeErr error // refuses every resume
}

func (f *fakeStarter) StartInteractive(_ context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	f.got = append(f.got, req)
	if f.err != nil {
		return nil, ports.AgentSpec{}, f.err
	}
	s, err := f.sessions.Create(&domain.Session{
		ID: fmt.Sprintf("peer-%d", len(f.got)), ProjectID: req.ProjectID, TicketID: req.TicketID, RepositoryID: req.RepositoryID,
		AgentID: req.AgentID, Status: domain.SessionRunning, Branch: "task/peer", ParentSessionID: req.ParentSessionID,
	})
	return s, ports.AgentSpec{}, err
}

// ResumeInteractive records the request and puts the session back to running,
// as orchestration would.
func (f *fakeStarter) ResumeInteractive(_ context.Context, req ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	f.resumed = append(f.resumed, req)
	if f.resumeErr != nil {
		return nil, ports.AgentSpec{}, f.resumeErr
	}
	if err := f.sessions.UpdateStatus(req.SessionID, domain.SessionRunning); err != nil {
		return nil, ports.AgentSpec{}, err
	}
	return f.sessions.Get(req.SessionID), ports.AgentSpec{}, nil
}
func (f *fakeStarter) EndInteractive(context.Context, string, int, bool) (*domain.Session, error) {
	return nil, nil
}

type repoList []*domain.Repository

func (r repoList) ListRepositories(context.Context, string) ([]*domain.Repository, error) {
	return r, nil
}

func startFixture(t *testing.T, autoRun bool) (start func(args map[string]any) (map[string]any, error), starter *fakeStarter, taskID string) {
	return startFixtureWith(t, autoRun, true)
}

func startFixtureWith(t *testing.T, autoRun, canStart bool) (start func(args map[string]any) (map[string]any, error), starter *fakeStarter, taskID string) {
	t.Helper()
	w := startWorldWith(t, autoRun, canStart)
	return w.start, w.starter, w.taskID
}

// startWorld is startFixture with the session store, so a test can seed the
// sessions a start joins.
type startWorld struct {
	start     func(args map[string]any) (map[string]any, error)
	starter   *fakeStarter
	sessions  ports.SessionRepository
	taskID    string
	projectID string
}

func startWorldWith(t *testing.T, autoRun, canStart bool) startWorld {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	task, _ := plan.CreateTicket(context.Background(), proj.ID, "Add SSE feed", "", domain.TicketStatusInProgress, "")
	if _, err := sessions.Create(&domain.Session{ID: "me", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api",
		Status: domain.SessionRunning, AutoRun: autoRun}); err != nil {
		t.Fatal(err)
	}
	starter := &fakeStarter{sessions: sessions}
	peers := PeerStarter{Sessions: starter, Repositories: repoList{{ID: "r-api", Name: "api"}, {ID: "r-web", Name: "web"}}}
	if !canStart {
		peers = PeerStarter{}
	}
	var tool domain.Tool
	for _, tl := range SessionTaskTools(plan, sessions, agentList{{ID: "a-rev", Name: "Reviewer"}}, nil, peers, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name == "start_task_session" {
			tool = tl
		}
	}
	if tool.Handler == nil {
		t.Fatal("start_task_session not built")
	}
	return startWorld{start: func(args map[string]any) (map[string]any, error) {
		out, err := tool.Handler(WithSessionID(context.Background(), "me"), args)
		if err != nil {
			return nil, err
		}
		return out.(map[string]any), nil
	}, starter: starter, sessions: sessions, taskID: task.ID, projectID: proj.ID}
}

func TestStartTaskSession_StartsAPeerOnTheCallersTask(t *testing.T) {
	start, starter, taskID := startFixture(t, false)
	out, err := start(map[string]any{"prompt": "write the tests for feed.go", "agent": "reviewer", "repository": "WEB", "base_branch": "task/me"})
	if err != nil {
		t.Fatal(err)
	}
	req := starter.got[0]
	want := ports.InteractiveRequest{
		ProjectID: req.ProjectID, RepositoryID: "r-web", TicketID: taskID, AgentID: "a-rev", Prompt: "write the tests for feed.go",
		BaseBranch: "task/me", RunsOn: domain.RunnerServer, ParentSessionID: "me", AutoAccept: "off",
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("request = %+v\nwant      %+v", req, want)
	}
	if out["session_id"] != "peer-1" || out["agent"] != "Reviewer" || out["repository"] != "web" {
		t.Fatalf("out = %+v", out)
	}

	// Defaults: the caller's repository, no agent.
	if _, err := start(map[string]any{"prompt": "x"}); err != nil {
		t.Fatal(err)
	}
	if req := starter.got[1]; req.RepositoryID != "r-api" || req.AgentID != "" {
		t.Fatalf("defaults = %+v", req)
	}
}

func TestStartTaskSession_ARemoteBaseReachesTheStartAndItsRefusalNamesTheBase(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": "x", "base_branch": " origin/feature-x "}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0].BaseBranch; got != "origin/feature-x" {
		t.Fatalf("base_branch = %q, want origin/feature-x", got)
	}

	starter.err = &domain.StructuredError{Code: domain.RemoteBranchNotFound,
		Message: "origin/feature-x is not on origin any more. Fetch from origin to refresh the list."}
	_, err := start(map[string]any{"prompt": "x", "base_branch": "origin/feature-x"})
	if err == nil || !strings.Contains(err.Error(), "origin/feature-x") || !strings.Contains(err.Error(), "not on origin any more") {
		t.Fatalf("refusal = %v, want a message naming the base and the reason", err)
	}
}

func TestStartTaskSession_ItsDescriptionSaysARemoteBaseIsFetchedFirst(t *testing.T) {
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name != "start_task_session" {
			continue
		}
		schema := strings.NewReplacer(`\u003c`, "<", `\u003e`, ">").Replace(string(mustJSON(t, tl.InputSchema)))
		for what, text := range map[string]string{"description": tl.Description, "base_branch": schema} {
			if !strings.Contains(text, "origin/<name>") || !strings.Contains(text, "fetched") {
				t.Errorf("the %s does not say a remote base origin/<name> is fetched first:\n%s", what, text)
			}
		}
		return
	}
	t.Fatal("start_task_session not built")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStartTaskSession_PassesTheMode(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": "shape the export feature", "mode": "architect"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0].Mode; got != "architect" {
		t.Fatalf("mode = %q, want architect", got)
	}
}

func TestStartTaskSession_OffersDesignMode(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": "make the card", "mode": "design"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0].Mode; got != "design" {
		t.Fatalf("mode = %q, want design", got)
	}
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name != "start_task_session" {
			continue
		}
		schema, _ := json.Marshal(tl.InputSchema)
		if !strings.Contains(string(schema), `"design"`) {
			t.Fatalf("start_task_session schema does not offer design: %s", schema)
		}
	}
}

// The architect spawns the verification modes (ADR 0090): the tool offers each
// of the four, describes it, and passes it through.
func TestStartTaskSession_OffersTheVerificationModes(t *testing.T) {
	var schema string
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name == "start_task_session" {
			b, _ := json.Marshal(tl.InputSchema)
			schema = string(b)
		}
	}
	for mode, meaning := range map[string]string{
		"review": "Reviewer", "test": "Tester", "qa": "QA", "operate": "Operator",
	} {
		start, starter, _ := startFixture(t, false)
		if _, err := start(map[string]any{"prompt": "check the branch", "mode": mode}); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if got := starter.got[0].Mode; got != mode {
			t.Errorf("mode = %q, want %q", got, mode)
		}
		if !strings.Contains(schema, `"`+mode+`"`) || !strings.Contains(schema, meaning) {
			t.Errorf("start_task_session schema does not offer %s as %s: %s", mode, meaning, schema)
		}
	}
}

// The architect hands each spec to a delegate in implement mode.
func TestStartTaskSession_OffersImplementMode(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": `Implement "Spec: backend — CSV export".`, "mode": "implement"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0].Mode; got != "implement" {
		t.Fatalf("mode = %q, want implement", got)
	}
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name != "start_task_session" {
			continue
		}
		schema, _ := json.Marshal(tl.InputSchema)
		if !strings.Contains(string(schema), `"implement"`) {
			t.Fatalf("start_task_session schema does not offer implement: %s", schema)
		}
	}
}

func TestStartTaskSession_AnUnattendedParentStartsAnUnattendedPeer(t *testing.T) {
	start, starter, _ := startFixture(t, true)
	if _, err := start(map[string]any{"prompt": "x"}); err != nil {
		t.Fatal(err)
	}
	if starter.got[0].AutoAccept != "all" {
		t.Fatalf("auto accept = %q", starter.got[0].AutoAccept)
	}
}

func TestStartTaskSession_Refusals(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	for _, c := range []struct {
		args map[string]any
		code string
	}{
		{map[string]any{"prompt": "  "}, "INVALID_INPUT"},
		{map[string]any{"prompt": "x", "agent": "nobody"}, "AGENT_NOT_FOUND"},
		{map[string]any{"prompt": "x", "repository": "nope"}, "REPOSITORY_NOT_FOUND"},
	} {
		if _, err := start(c.args); errs.Code(err) != c.code {
			t.Errorf("%v: got %v, want %s", c.args, err, c.code)
		}
	}
	if len(starter.got) != 0 {
		t.Fatalf("a refused call started something: %+v", starter.got)
	}
}

func TestStartTaskSession_CapsTheLiveSessionsOnATask(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	for i := 1; i < MaxLiveTaskSessions; i++ { // the caller is the first: the 15th start makes 16
		if _, err := start(map[string]any{"prompt": "x"}); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	_, err := start(map[string]any{"prompt": "one too many"})
	if errs.Code(err) != "TASK_SESSION_LIMIT" {
		t.Fatalf("over the limit: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "16 sessions already run") || !strings.Contains(msg, "the limit is 16") {
		t.Fatalf("the refusal names neither the count nor the limit: %q", msg)
	}
	if len(starter.got) != MaxLiveTaskSessions-1 {
		t.Fatalf("started %d", len(starter.got))
	}
}

func TestStartTaskSession_UnavailableWithoutAStarter(t *testing.T) {
	start, _, _ := startFixtureWith(t, false, false)
	if _, err := start(map[string]any{"prompt": "x"}); errs.Code(err) != "UNAVAILABLE" {
		t.Fatalf("got %v", err)
	}
}

// A product session shapes a feature into a PRD and child tasks; the tool
// offers the mode and passes it through.
func TestStartTaskSession_OffersProductMode(t *testing.T) {
	start, starter, _ := startFixture(t, false)
	if _, err := start(map[string]any{"prompt": "shape the export feature", "mode": "product"}); err != nil {
		t.Fatal(err)
	}
	if got := starter.got[0].Mode; got != "product" {
		t.Fatalf("mode = %q, want product", got)
	}
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name != "start_task_session" {
			continue
		}
		schema, _ := json.Marshal(tl.InputSchema)
		if !strings.Contains(string(schema), `"product"`) || !strings.Contains(string(schema), "product: ") {
			t.Fatalf("start_task_session schema does not offer and describe product: %s", schema)
		}
	}
}

// A session an agent starts with start_task_session runs on the server's own
// terminal host, in whatever mode: the kind a restart interrupts and a resume
// brings back (ADR 0082).
func TestStartTaskSession_EveryModeStartsAServerRunSession(t *testing.T) {
	for _, mode := range []string{"", "architect", "product", "implement", "design", "review", "test", "qa", "operate"} {
		start, starter, _ := startFixture(t, false)
		if _, err := start(map[string]any{"prompt": "go", "mode": mode}); err != nil {
			t.Fatalf("mode %q: %v", mode, err)
		}
		if got := starter.got[0].RunsOn; got != domain.RunnerServer {
			t.Errorf("mode %q starts on %q, want the server", mode, got)
		}
	}
}

// joinWorld seeds the sessions a delegate can join: an implementer running in
// worktree w-impl of the caller's task, an ended one, one with its worktree
// cleared, and one on another task.
func joinWorld(t *testing.T) startWorld {
	t.Helper()
	w := startWorldWith(t, false, true)
	for _, s := range []*domain.Session{
		{ID: "impl", Status: domain.SessionRunning, WorkspaceID: "w-impl", Branch: "task/impl", TicketID: w.taskID},
		{ID: "ended", Status: domain.SessionDone, WorkspaceID: "w-ended", Branch: "task/ended", TicketID: w.taskID},
		{ID: "cleared", Status: domain.SessionDone, WorkspaceID: "w-cleared", Branch: "task/cleared", TicketID: w.taskID},
		{ID: "other-task", Status: domain.SessionRunning, WorkspaceID: "w-other", Branch: "task/other", TicketID: "another-task"},
		{ID: "web-impl", Status: domain.SessionRunning, WorkspaceID: "w-web", Branch: "task/web", TicketID: w.taskID, RepositoryID: "r-web"},
	} {
		s.ProjectID = w.projectID
		if s.RepositoryID == "" {
			s.RepositoryID = "r-api"
		}
		if _, err := w.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	// Clearing a worktree records when and blanks the session's workspace, as
	// the store does for real.
	if err := w.sessions.UpdateWorktreeCleared("cleared", time.Now()); err != nil {
		t.Fatal(err)
	}
	return w
}

func TestStartTaskSession_JoinsTheNamedSessionsWorktreeWithTheModesDefaultAccess(t *testing.T) {
	for _, c := range []struct {
		mode   string
		access string
		want   domain.SessionAccess
	}{
		{"review", "", domain.AccessReadOnly},
		{"qa", "", domain.AccessReadOnly},
		{"implement", "", domain.AccessWrite},
		{"test", "", domain.AccessWrite},
		{"operate", "", domain.AccessWrite},
		{"", "", domain.AccessWrite},
		{"review", "write", domain.AccessWrite},
		{"implement", "read_only", domain.AccessReadOnly},
	} {
		w := joinWorld(t)
		if _, err := w.start(map[string]any{"prompt": "x", "mode": c.mode, "access": c.access, "join_session_id": "impl"}); err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		req := w.starter.got[0]
		if req.WorktreeID != "w-impl" || req.Access != string(c.want) || req.BaseBranch != "" || req.BranchName != "" {
			t.Errorf("%+v: request = %+v", c, req)
		}
	}
}

func TestStartTaskSession_JoinsAnEndedSessionsWorktree(t *testing.T) {
	w := joinWorld(t)
	if _, err := w.start(map[string]any{"prompt": "x", "mode": "review", "join_session_id": "ended"}); err != nil {
		t.Fatal(err)
	}
	if req := w.starter.got[0]; req.WorktreeID != "w-ended" {
		t.Fatalf("request = %+v", req)
	}
}

func TestStartTaskSession_AJoinTakesTheJoinedSessionsRepositoryAndCountsTowardsTheLimit(t *testing.T) {
	w := joinWorld(t)
	if _, err := w.start(map[string]any{"prompt": "x", "join_session_id": "web-impl"}); err != nil {
		t.Fatal(err)
	}
	if req := w.starter.got[0]; req.RepositoryID != "r-web" {
		t.Fatalf("the join did not take the joined session's repository: %+v", req)
	}
	// "web" names the joined session's own repository: fine.
	if _, err := w.start(map[string]any{"prompt": "x", "join_session_id": "web-impl", "repository": "web"}); err != nil {
		t.Fatal(err)
	}
	for i := len(w.starter.got); i <= MaxLiveTaskSessions; i++ { // fills the task to its limit
		if _, err := w.start(map[string]any{"prompt": "x", "join_session_id": "impl"}); err != nil {
			if errs.Code(err) != "TASK_SESSION_LIMIT" {
				t.Fatalf("start %d: %v", i, err)
			}
			return
		}
	}
	t.Fatal("joined sessions did not count towards the limit")
}

func TestStartTaskSession_AJoinRefusalNamesTheSessionAndTheReasonAndStartsNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want []string // all must appear in the message
	}{
		{"another task", map[string]any{"join_session_id": "other-task"}, []string{"other-task", "another task"}},
		{"unknown session", map[string]any{"join_session_id": "nope"}, []string{"nope", "no session"}},
		{"cleared worktree", map[string]any{"join_session_id": "cleared"}, []string{"cleared", "worktree", "cleared or is gone"}},
		{"with a base branch", map[string]any{"join_session_id": "impl", "base_branch": "main"}, []string{"impl", "base_branch", "cannot be combined"}},
		{"bad access", map[string]any{"join_session_id": "impl", "access": "admin"}, []string{"admin", "write", "read_only"}},
		{"other repository", map[string]any{"join_session_id": "impl", "repository": "web"}, []string{"impl", "web", "api"}},
		{"access without a join", map[string]any{"access": "read_only"}, []string{"access", "join_session_id"}},
	} {
		w := joinWorld(t)
		args := map[string]any{"prompt": "x"}
		for k, v := range c.args {
			args[k] = v
		}
		_, err := w.start(args)
		if err == nil {
			t.Errorf("%s: not refused", c.name)
			continue
		}
		for _, part := range c.want {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: message %q lacks %q", c.name, err.Error(), part)
			}
		}
		if len(w.starter.got) != 0 {
			t.Errorf("%s: a refused call started something: %+v", c.name, w.starter.got)
		}
	}
}

// The start path's own refusals of a join, as orchestration raises them (a
// coded error with the form's wording), reach the architect naming the session
// and the reason, not in the form's words.
func TestStartTaskSession_TheStartPathsJoinRefusalsNameTheSessionAndTheReason(t *testing.T) {
	refusal := func(code, message string) error {
		return &domain.DetailedError{StructuredError: &domain.StructuredError{Code: code, Message: code + ": " + message}}
	}
	for _, c := range []struct {
		name    string
		mode    string
		refused error
		want    []string
		not     []string
	}{
		{"gone from disk", "implement",
			refusal("WORKTREE_NOT_FOUND", "That worktree is gone from disk since the list was read. Nothing was started. Pick another or start on a new branch."),
			[]string{"impl", "gone from disk"}, []string{"since the list was read", "Pick another"}},
		{"read-only with the sandbox off", "review",
			refusal("ACCESS_INVALID", "Read-only access needs the agent sandbox; it is off for this session."),
			[]string{"impl", "sandbox", "pass access: write to join with write access"}, nil},
	} {
		w := joinWorld(t)
		w.starter.err = c.refused
		_, err := w.start(map[string]any{"prompt": "x", "mode": c.mode, "join_session_id": "impl"})
		if err == nil {
			t.Fatalf("%s: not refused", c.name)
		}
		for _, part := range c.want {
			if !strings.Contains(err.Error(), part) {
				t.Errorf("%s: message %q lacks %q", c.name, err.Error(), part)
			}
		}
		for _, part := range c.not {
			if strings.Contains(err.Error(), part) {
				t.Errorf("%s: message %q still says %q", c.name, err.Error(), part)
			}
		}
		if errs.Code(err) != errs.Code(c.refused) {
			t.Errorf("%s: code %q, want %q", c.name, errs.Code(err), errs.Code(c.refused))
		}
	}
}

func TestStartTaskSession_AnArchitectProductOrDesignSystemSessionCannotJoinAndTheMessageSaysWho(t *testing.T) {
	for _, mode := range []string{"architect", "product"} {
		w := joinWorld(t)
		_, err := w.start(map[string]any{"prompt": "x", "mode": mode, "join_session_id": "impl"})
		if err == nil || !strings.Contains(err.Error(), "impl") || !strings.Contains(err.Error(), mode) || !strings.Contains(err.Error(), "cannot join") {
			t.Errorf("%s: got %v", mode, err)
		}
		if len(w.starter.got) != 0 {
			t.Errorf("%s: started %+v", mode, w.starter.got)
		}
	}
}

func TestStartTaskSession_TheDescriptionNamesTheJoinOptionItsAccessAndTheLimit(t *testing.T) {
	var tool domain.Tool
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, NotificationTooling{}) {
		if tl.Name == "start_task_session" {
			tool = tl
		}
	}
	for _, want := range []string{"join_session_id", "access", "read_only", "16", "second implementer", "verification"} {
		if !strings.Contains(tool.Description+string(mustJSON(t, tool.InputSchema)), want) {
			t.Errorf("the tool does not say %q", want)
		}
	}
}

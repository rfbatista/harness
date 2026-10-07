package tooling

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeStarter records what it was asked to start and records the session, as
// orchestration would.
type fakeStarter struct {
	sessions ports.SessionRepository
	got      []ports.InteractiveRequest
}

func (f *fakeStarter) StartInteractive(_ context.Context, req ports.InteractiveRequest) (*domain.Session, ports.AgentSpec, error) {
	f.got = append(f.got, req)
	s, err := f.sessions.Create(&domain.Session{
		ID: fmt.Sprintf("peer-%d", len(f.got)), ProjectID: req.ProjectID, TicketID: req.TicketID, RepositoryID: req.RepositoryID,
		AgentID: req.AgentID, Status: domain.SessionRunning, Branch: "task/peer", ParentSessionID: req.ParentSessionID,
	})
	return s, ports.AgentSpec{}, err
}
func (f *fakeStarter) ResumeInteractive(context.Context, ports.ResumeRequest) (*domain.Session, ports.AgentSpec, error) {
	return nil, ports.AgentSpec{}, nil
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
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	sessions := sqlite.NewSessionRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	proj, _ := projects.Create("p", t.TempDir())
	task, _ := plan.CreateTicket(context.Background(), proj.ID, "Add SSE feed", "", domain.TicketStatusInProgress)
	if _, err := sessions.Create(&domain.Session{ID: "me", ProjectID: proj.ID, TicketID: task.ID, RepositoryID: "r-api",
		Status: domain.SessionRunning, AutoRun: autoRun}); err != nil {
		t.Fatal(err)
	}
	starter = &fakeStarter{sessions: sessions}
	peers := PeerStarter{Sessions: starter, Repositories: repoList{{ID: "r-api", Name: "api"}, {ID: "r-web", Name: "web"}}}
	if !canStart {
		peers = PeerStarter{}
	}
	var tool domain.Tool
	for _, tl := range SessionTaskTools(plan, sessions, agentList{{ID: "a-rev", Name: "Reviewer"}}, nil, peers, ArtifactTooling{}, nil) {
		if tl.Name == "start_task_session" {
			tool = tl
		}
	}
	if tool.Handler == nil {
		t.Fatal("start_task_session not built")
	}
	return func(args map[string]any) (map[string]any, error) {
		out, err := tool.Handler(WithSessionID(context.Background(), "me"), args)
		if err != nil {
			return nil, err
		}
		return out.(map[string]any), nil
	}, starter, task.ID
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
	if req != want {
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
	for _, tl := range SessionTaskTools(nil, nil, nil, nil, PeerStarter{}, ArtifactTooling{}, nil) {
		if tl.Name != "start_task_session" {
			continue
		}
		schema, _ := json.Marshal(tl.InputSchema)
		if !strings.Contains(string(schema), `"design"`) {
			t.Fatalf("start_task_session schema does not offer design: %s", schema)
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
	for i := 1; i < MaxLiveTaskSessions; i++ { // the caller is the first
		if _, err := start(map[string]any{"prompt": "x"}); err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
	}
	if _, err := start(map[string]any{"prompt": "one too many"}); errs.Code(err) != "TASK_SESSION_LIMIT" {
		t.Fatalf("over the limit: %v", err)
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

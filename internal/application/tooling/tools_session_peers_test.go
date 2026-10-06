package tooling

import (
	"context"
	"encoding/json"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
)

type agentList []*domain.Agent

func (a agentList) ListAgents(context.Context) ([]*domain.Agent, error) { return a, nil }
func (a agentList) GetAgent(context.Context, string) (*domain.Agent, error) {
	return nil, nil
}

// peersFixture: one task worked on by the caller and three others (two live,
// one done), plus a session on another task that must stay out of view.
func peersFixture(t *testing.T) (call func(sessionID string, args map[string]any) map[string]any) {
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
	other, _ := plan.CreateTicket(context.Background(), proj.ID, "Write docs", "", domain.TicketStatusTodo)

	for _, s := range []*domain.Session{
		{ID: "me", ProjectID: proj.ID, TicketID: task.ID, Task: "implement", Status: domain.SessionRunning, Branch: "feat/me"},
		{ID: "reviewer", ProjectID: proj.ID, TicketID: task.ID, AgentID: "a-rev", Task: "review the plan", Status: domain.SessionIdle,
			LastAction: "Read plan.md", Branch: "feat/review", WorkingDir: "/w/review", Interactive: true},
		{ID: "plain", ProjectID: proj.ID, TicketID: task.ID, Task: "write tests", Status: domain.SessionThinking, Branch: "feat/tests"},
		{ID: "finished", ProjectID: proj.ID, TicketID: task.ID, AgentID: "a-gone", Task: "spike", Status: domain.SessionDone},
		{ID: "elsewhere", ProjectID: proj.ID, TicketID: other.ID, Task: "docs", Status: domain.SessionRunning},
	} {
		if _, err := sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	// The last action is recorded as the session runs, not at creation.
	if err := sessions.UpdateMetrics("reviewer", 0, 0, 0, "Read plan.md", 0); err != nil {
		t.Fatal(err)
	}

	var tool domain.Tool
	for _, tl := range SessionTaskTools(plan, sessions, agentList{{ID: "a-rev", Name: "Reviewer"}}, nil, PeerStarter{}, ArtifactTooling{}) {
		if tl.Name == "list_task_sessions" {
			tool = tl
		}
	}
	if tool.Handler == nil {
		t.Fatal("list_task_sessions not built")
	}
	return func(sessionID string, args map[string]any) map[string]any {
		t.Helper()
		out, err := tool.Handler(WithSessionID(context.Background(), sessionID), args)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out) // read it as the agent does: JSON
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
}

func ids(out map[string]any) []string {
	var got []string
	for _, s := range out["sessions"].([]any) {
		got = append(got, s.(map[string]any)["session_id"].(string))
	}
	return got
}

func TestListTaskSessions_ShowsTheOtherLiveSessionsOnTheTask(t *testing.T) {
	call := peersFixture(t)
	out := call("me", nil)

	if got := ids(out); len(got) != 2 || !contains(got, "reviewer") || !contains(got, "plain") {
		t.Fatalf("sessions = %v, want the two live peers (not me, not finished, not another task's)", got)
	}
	if you := out["you"].(map[string]any); you["session_id"] != "me" || you["branch"] != "feat/me" {
		t.Errorf("you = %v", you)
	}
	if task := out["task"].(map[string]any); task["title"] != "Add SSE feed" {
		t.Errorf("task = %v", task)
	}

	for _, s := range out["sessions"].([]any) {
		peer := s.(map[string]any)
		switch peer["session_id"] {
		case "reviewer":
			if peer["agent_name"] != "Reviewer" || peer["brief"] != "review the plan" || peer["last_action"] != "Read plan.md" ||
				peer["branch"] != "feat/review" || peer["worktree"] != "/w/review" || peer["running"] != true || peer["interactive"] != true {
				t.Errorf("reviewer = %v", peer)
			}
		case "plain":
			if peer["agent_name"] != "plain claude" {
				t.Errorf("a session without an agent reads as plain claude: %v", peer)
			}
		}
	}
}

func TestListTaskSessions_IncludeEndedListsThemLast(t *testing.T) {
	call := peersFixture(t)
	got := ids(call("me", map[string]any{"include_ended": true}))
	if len(got) != 3 || got[2] != "finished" {
		t.Fatalf("sessions = %v, want the live peers then the finished one", got)
	}
	out := call("me", map[string]any{"include_ended": true})
	last := out["sessions"].([]any)[2].(map[string]any)
	if last["agent_name"] != "a-gone" || last["running"] != false {
		t.Errorf("an agent that no longer exists reads as its id: %v", last)
	}
}

func TestListTaskSessions_IsScopedToTheCallersTask(t *testing.T) {
	call := peersFixture(t)
	got := ids(call("elsewhere", map[string]any{"include_ended": true}))
	if len(got) != 0 {
		t.Fatalf("the docs task has no other sessions, got %v", got)
	}
}

func TestListTaskSessions_RefusesWithoutASession(t *testing.T) {
	db, _ := sqlite.Open(":memory:")
	projects := sqlite.NewProjectRepository(db)
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	for _, tl := range SessionTaskTools(plan, sqlite.NewSessionRepository(db), nil, nil, PeerStarter{}, ArtifactTooling{}) {
		if tl.Name != "list_task_sessions" {
			continue
		}
		_, err := tl.Handler(context.Background(), nil)
		wantCode(t, err, "SESSION_NOT_FOUND")
		_, err = tl.Handler(WithSessionID(context.Background(), "ghost"), nil)
		wantCode(t, err, "SESSION_NOT_FOUND")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

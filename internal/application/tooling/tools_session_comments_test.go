package tooling

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// namedAgents is an agent catalog that knows go-developer by id a1 and
// fails for anything else.
type namedAgents struct{}

func (namedAgents) ListAgents(context.Context) ([]*domain.Agent, error) {
	return []*domain.Agent{{ID: "a1", Name: "go-developer"}}, nil
}

func (namedAgents) GetAgent(_ context.Context, id string) (*domain.Agent, error) {
	if id == "a1" {
		return &domain.Agent{ID: "a1", Name: "go-developer"}, nil
	}
	return nil, errors.New("agent lookup failed")
}

// commentFixture is the task tools fixture with an agent catalog and three
// sessions on the task: sess-1 (plain), sess-agent (go-developer) and
// sess-gone (an agent the catalog cannot find), plus one with no task.
func newCommentFixture(t *testing.T) *taskToolsFixture {
	t.Helper()
	f := newTaskToolsFixture(t)
	for _, s := range []*domain.Session{
		{ID: "sess-agent", ProjectID: f.projectID, TicketID: f.ticketID, AgentID: "a1", Status: domain.SessionRunning},
		{ID: "sess-gone", ProjectID: f.projectID, TicketID: f.ticketID, AgentID: "a-deleted", Status: domain.SessionRunning},
		{ID: "sess-loose", ProjectID: f.projectID, Status: domain.SessionRunning},
	} {
		if _, err := f.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, tl := range SessionTaskTools(f.plan, f.sessions, namedAgents{}, nil, PeerStarter{}, ArtifactTooling{}, nil,
		ResourceTooling{Files: f.plan, Comments: f.plan, Writer: f.plan}, NotificationTooling{}) {
		f.tools[tl.Name] = tl
	}
	return f
}

func (f *taskToolsFixture) addComment(t *testing.T, sessionID, body string) *domain.TicketComment {
	t.Helper()
	out, err := f.call(t, sessionID, "add_task_comment", map[string]any{"body": body})
	if err != nil {
		t.Fatal(err)
	}
	return out.(map[string]any)["comment"].(*domain.TicketComment)
}

func TestAddTaskComment_WritesAsTheCallingSession(t *testing.T) {
	f := newCommentFixture(t)
	for _, c := range []struct{ session, agent string }{
		{"sess-agent", "go-developer"},
		{"sess-1", ""},             // a plain session
		{"sess-gone", "a-deleted"}, // the name lookup failed: the agent id
	} {
		got := f.addComment(t, c.session, "  note from "+c.session+"  ")
		if got.Author != domain.CommentBySession || got.SessionID != c.session || got.AgentName != c.agent ||
			got.TicketID != f.ticketID || got.Body != "note from "+c.session {
			t.Errorf("%s wrote %+v", c.session, got)
		}
	}
	// What the agent sends cannot name another author.
	out, err := f.call(t, "sess-1", "add_task_comment", map[string]any{"body": "x", "session_id": "sess-agent", "author": "person"})
	if err != nil {
		t.Fatal(err)
	}
	if c := out.(map[string]any)["comment"].(*domain.TicketComment); c.SessionID != "sess-1" || c.Author != domain.CommentBySession {
		t.Fatalf("arguments chose the author: %+v", c)
	}
}

func TestAddTaskComment_Refusals(t *testing.T) {
	f := newCommentFixture(t)
	_, err := f.call(t, "sess-1", "add_task_comment", map[string]any{"body": "  "})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "sess-1", "add_task_comment", map[string]any{"body": strings.Repeat("x", domain.MaxCommentLen+1)})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "sess-loose", "add_task_comment", map[string]any{"body": "x"})
	wantCode(t, err, "SESSION_HAS_NO_TASK")
	_, err = f.call(t, "", "add_task_comment", map[string]any{"body": "x"})
	wantCode(t, err, "SESSION_NOT_FOUND")
}

func TestUpdateAndDeleteTaskComment_OnlyTheWritersOwn(t *testing.T) {
	f := newCommentFixture(t)
	ctx := context.Background()
	mine := f.addComment(t, "sess-1", "mine")
	theirs := f.addComment(t, "sess-agent", "theirs")
	persons, _ := f.plan.AddTicketComment(ctx, f.ticketID, "the person's")
	other, _ := f.plan.CreateTicket(ctx, f.projectID, "Other", "", domain.TicketStatusTodo)
	away, _ := f.plan.AddSessionComment(ctx, other.ID, domain.CommentWriter{SessionID: "sess-1"}, "elsewhere")

	time.Sleep(2 * time.Millisecond)
	out, err := f.call(t, "sess-1", "update_task_comment", map[string]any{"comment_id": mine.ID, "body": "rewritten"})
	if err != nil {
		t.Fatal(err)
	}
	if c := out.(map[string]any)["comment"].(*domain.TicketComment); c.Body != "rewritten" || !c.UpdatedAt.After(c.CreatedAt) {
		t.Fatalf("updated = %+v", c)
	}

	for name, tc := range map[string]struct{ id, code string }{
		"missing":           {"missing", "COMMENT_NOT_FOUND"},
		"another task's":    {away.ID, "COMMENT_NOT_ON_TASK"},
		"another session's": {theirs.ID, "COMMENT_NOT_YOURS"},
		"the person's":      {persons.ID, "COMMENT_NOT_YOURS"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.call(t, "sess-1", "update_task_comment", map[string]any{"comment_id": tc.id, "body": "x"})
			wantCode(t, err, tc.code)
			_, err = f.call(t, "sess-1", "delete_task_comment", map[string]any{"comment_id": tc.id})
			wantCode(t, err, tc.code)
		})
	}
	_, err = f.call(t, "sess-1", "update_task_comment", map[string]any{"body": "x"})
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.call(t, "sess-1", "delete_task_comment", map[string]any{})
	wantCode(t, err, "INVALID_INPUT")

	out, err = f.call(t, "sess-1", "delete_task_comment", map[string]any{"comment_id": mine.ID})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := out.(map[string]any); !ok || len(m) != 0 {
		t.Fatalf("delete answers {}: %#v", out)
	}
	_, err = f.call(t, "sess-1", "delete_task_comment", map[string]any{"comment_id": mine.ID})
	wantCode(t, err, "COMMENT_NOT_FOUND")
}

func TestListTaskComments_BothAuthors(t *testing.T) {
	f := newCommentFixture(t)
	p, _ := f.plan.AddTicketComment(context.Background(), f.ticketID, "from the person")
	s := f.addComment(t, "sess-agent", "from an agent")
	out, err := f.call(t, "sess-1", "list_task_comments", nil)
	if err != nil {
		t.Fatal(err)
	}
	list := out.(map[string]any)["comments"].([]*domain.TicketComment)
	if len(list) != 2 || list[0].ID != p.ID || list[0].Author != domain.CommentByPerson ||
		list[1].ID != s.ID || list[1].Author != domain.CommentBySession || list[1].AgentName != "go-developer" {
		t.Fatalf("list = %+v", list)
	}
}

func TestCommentWriteToolsUnavailableWithoutTheirPort(t *testing.T) {
	f := newTaskToolsFixture(t)
	for _, tl := range SessionTaskTools(f.plan, f.sessions, nil, nil, PeerStarter{}, ArtifactTooling{}, nil, ResourceTooling{Files: f.plan, Comments: f.plan}, NotificationTooling{}) {
		switch tl.Name {
		case "add_task_comment", "update_task_comment", "delete_task_comment":
			_, err := tl.Handler(WithSessionID(context.Background(), f.sessionID), map[string]any{"body": "x", "comment_id": "c"})
			wantCode(t, err, "UNAVAILABLE")
		}
	}
}

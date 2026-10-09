package planning_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionTaskComments = (*planning.Service)(nil)

var (
	writer = domain.CommentWriter{SessionID: "s1", AgentName: "go-developer"}
	other  = domain.CommentWriter{SessionID: "s2"}
)

func TestASessionCommentsAsItself(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	c, err := f.svc.AddSessionComment(ctx, f.ticketID, writer, "  progress\nnote  ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Author != domain.CommentBySession || c.SessionID != "s1" || c.AgentName != "go-developer" ||
		c.Body != "progress\nnote" || c.TicketID != f.ticketID || c.ProjectID == "" {
		t.Fatalf("added = %+v", c)
	}
	plain, err := f.svc.AddSessionComment(ctx, f.ticketID, other, "plain")
	if err != nil || plain.AgentName != "" || plain.SessionID != "s2" {
		t.Fatalf("a plain session's comment = %+v, %v", plain, err)
	}
	list, _ := f.svc.ListTicketComments(ctx, f.ticketID, time.Time{})
	if len(list) != 2 || list[0].SessionID != "s1" {
		t.Fatalf("list = %+v", list)
	}
	wire := f.feed.wire(t)
	if len(wire) != 2 || !strings.Contains(wire[0], `"author":"session"`) || !strings.Contains(wire[0], `"session_id":"s1"`) ||
		!strings.Contains(wire[0], `"agent_name":"go-developer"`) {
		t.Fatalf("feed = %v", wire)
	}
	if len(f.events) != 2 {
		t.Fatalf("every comment raises its event; orchestration decides who hears: %+v", f.events)
	}
	if ev, ok := f.events[0].(domain.TicketCommentAdded); !ok || ev.Comment.Author != domain.CommentBySession || ev.Comment.SessionID != "s1" {
		t.Fatalf("event = %+v", f.events[0])
	}
}

func TestASessionAddRefusals(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	_, err := f.svc.AddSessionComment(ctx, "missing", writer, "x")
	wantCode(t, err, "TICKET_NOT_FOUND")
	_, err = f.svc.AddSessionComment(ctx, f.ticketID, writer, " \n ")
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.AddSessionComment(ctx, f.ticketID, writer, strings.Repeat("x", domain.MaxCommentLen+1))
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.AddSessionComment(ctx, f.ticketID, domain.CommentWriter{}, "x")
	wantCode(t, err, "INVALID_INPUT")
}

func TestASessionEditsAndDeletesOnlyItsOwnComments(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	mine, _ := f.svc.AddSessionComment(ctx, f.ticketID, writer, "mine")
	theirs, _ := f.svc.AddSessionComment(ctx, f.ticketID, other, "theirs")
	persons, _ := f.svc.AddTicketComment(ctx, f.ticketID, "the person's")
	tk, _ := f.svc.GetTicket(ctx, f.ticketID)
	elsewhere, err := f.svc.CreateTicket(ctx, tk.ProjectID, "Other", "", "")
	if err != nil {
		t.Fatal(err)
	}
	away, _ := f.svc.AddSessionComment(ctx, elsewhere.ID, writer, "on another task")

	same, err := f.svc.UpdateSessionComment(ctx, f.ticketID, "s1", mine.ID, "mine")
	if err != nil || !same.UpdatedAt.Equal(mine.UpdatedAt) {
		t.Fatalf("a no-op edit keeps updated_at: %+v, %v", same, err)
	}
	edited, err := f.svc.UpdateSessionComment(ctx, f.ticketID, "s1", mine.ID, " changed ")
	if err != nil || edited.Body != "changed" || !edited.UpdatedAt.After(edited.CreatedAt) ||
		edited.SessionID != "s1" || edited.AgentName != "go-developer" {
		t.Fatalf("edited = %+v, %v", edited, err)
	}

	for name, tc := range map[string]struct {
		id, code string
	}{
		"missing":           {"missing", "COMMENT_NOT_FOUND"},
		"another task's":    {away.ID, "COMMENT_NOT_ON_TASK"},
		"another session's": {theirs.ID, "COMMENT_NOT_YOURS"},
		"the person's":      {persons.ID, "COMMENT_NOT_YOURS"},
	} {
		_, err := f.svc.UpdateSessionComment(ctx, f.ticketID, "s1", tc.id, "x")
		if errs.Code(err) != tc.code {
			t.Errorf("update %s = %v, want %s", name, err, tc.code)
		}
		if err := f.svc.DeleteSessionComment(ctx, f.ticketID, "s1", tc.id); errs.Code(err) != tc.code {
			t.Errorf("delete %s = %v, want %s", name, err, tc.code)
		}
	}
	_, err = f.svc.UpdateSessionComment(ctx, f.ticketID, "s1", mine.ID, " ")
	wantCode(t, err, "INVALID_INPUT")
	_, err = f.svc.UpdateSessionComment(ctx, f.ticketID, "s1", "", "x")
	wantCode(t, err, "INVALID_INPUT")
	wantCode(t, f.svc.DeleteSessionComment(ctx, f.ticketID, "s1", ""), "INVALID_INPUT")

	if err := f.svc.DeleteSessionComment(ctx, f.ticketID, "s1", mine.ID); err != nil {
		t.Fatal(err)
	}
	wantCode(t, f.svc.DeleteSessionComment(ctx, f.ticketID, "s1", mine.ID), "COMMENT_NOT_FOUND")
	wire := f.feed.wire(t)
	if last := wire[len(wire)-1]; !strings.Contains(last, `"deleted":true`) || !strings.Contains(last, mine.ID) {
		t.Fatalf("the delete is announced: %v", last)
	}
}

func TestThePersonDeletesButCannotEditASessionsComment(t *testing.T) {
	f := newResourceFixture(t)
	ctx := context.Background()
	c, _ := f.svc.AddSessionComment(ctx, f.ticketID, writer, "agent note")
	_, err := f.svc.UpdateTicketComment(ctx, c.ID, "rewritten")
	wantCode(t, err, "COMMENT_NOT_YOURS")
	if got, _ := f.svc.ListTicketComments(ctx, f.ticketID, time.Time{}); got[0].Body != "agent note" {
		t.Fatalf("the refused edit changed the body: %+v", got[0])
	}
	if err := f.svc.DeleteTicketComment(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.ListTicketComments(ctx, f.ticketID, time.Time{}); len(got) != 0 {
		t.Fatalf("after the person's delete = %+v", got)
	}
}

func TestSessionCommentsUnconfiguredAreUnavailable(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	_, err := svc.AddSessionComment(ctx, "t", writer, "x")
	wantCode(t, err, "UNAVAILABLE")
	_, err = svc.UpdateSessionComment(ctx, "t", "s1", "c", "x")
	wantCode(t, err, "UNAVAILABLE")
	wantCode(t, svc.DeleteSessionComment(ctx, "t", "s1", "c"), "UNAVAILABLE")
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if errs.Code(err) != code {
		t.Fatalf("err = %v, want %s", err, code)
	}
}

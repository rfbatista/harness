package tasks

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

func TestBoardHasEveryColumnInOrderWithTheTasksAndTheirActivity(t *testing.T) {
	tasks := []*domain.Ticket{
		{ID: "t-done", Title: "Ship it", Status: domain.TicketStatusDone},
		{ID: "t-feed", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-port", Title: "Port tickets", Status: domain.TicketStatusInProgress},
	}
	sessions := []*domain.Session{
		{ID: "s1", TicketID: "t-feed", Status: domain.SessionRunning},
		{ID: "s2", TicketID: "t-feed", Status: domain.SessionIdle}, // waiting on you
		{ID: "s3", TicketID: "t-port", Status: domain.SessionDone},
		{ID: "s4", TicketID: "", Status: domain.SessionRunning}, // no task
	}
	b := BuildBoard("p1", tasks, sessions)

	var got []string
	for _, c := range b.Columns {
		var cards []string
		for _, l := range c.Cards {
			cards = append(cards, fmt.Sprintf("%s live=%d attention=%v", l.Label, l.Live, l.Attention))
		}
		got = append(got, c.Status+"/"+c.Label+": "+strings.Join(cards, ", "))
	}
	want := []string{
		"backlog/Backlog: ",
		"todo/Todo: ",
		"in_progress/In progress: Add SSE feed live=2 attention=true, Port tickets live=0 attention=false",
		"review/Review: ",
		"done/Done: Ship it live=0 attention=false",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("board:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	card := b.Columns[2].Cards[0]
	if card.TaskID != "t-feed" || card.Href != "/projects/p1/tasks/t-feed" || card.LiveState != "waiting" || card.LiveWord != "waiting" {
		t.Errorf("card = %+v", card)
	}
	if b.Empty() {
		t.Error("a board with tasks is not empty")
	}
}

func TestBoardWithoutTasksIsEmptyButKeepsItsColumns(t *testing.T) {
	b := BuildBoard("p1", nil, nil)
	if !b.Empty() || len(b.Columns) != 5 {
		t.Fatalf("board = %+v", b)
	}
}

func TestBoardCardsSayWhoPutThemThereAndForHowLong(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	defer func(was func() time.Time) { now = was }(now)
	now = func() time.Time { return at }
	tasks := []*domain.Ticket{
		{ID: "t-review", Title: "Check me", Status: domain.TicketStatusReview, CreatedAt: at.Add(-50 * time.Hour)},
		{ID: "t-todo", Title: "Later", Status: domain.TicketStatusTodo, CreatedAt: at.Add(-4 * time.Minute), PendingReviews: 1},
	}
	sessions := []*domain.Session{{ID: "s1", TicketID: "t-todo", Status: domain.SessionRunning}}
	b := BuildBoard("p1", tasks, sessions)

	review := b.Columns[3]
	if !review.Waiting() || b.Columns[1].Waiting() {
		t.Errorf("waiting: review=%v todo=%v", review.Waiting(), b.Columns[1].Waiting())
	}
	c := review.Cards[0]
	if c.Lead != "Created by" || c.Who != "you" || c.Age != "2d · stale" || !c.Stale || c.Merge != nil || c.HasSignals() {
		t.Errorf("review card = %+v", c)
	}
	todo := b.Columns[1].Cards[0]
	if todo.Age != "4m" || todo.LiveState != "running" || todo.LiveWord != "1 live" || !todo.HasSignals() {
		t.Errorf("todo card = %+v", todo)
	}
	if got := []string{b.Columns[0].EmptyText(), b.Columns[4].EmptyText()}; got[0] != "No tasks" || got[1] != "No done tasks. Drag a checked task here from Review." {
		t.Errorf("empty texts = %q", got)
	}
}

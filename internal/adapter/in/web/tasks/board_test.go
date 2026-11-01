package tasks

import (
	"encoding/json"
	"slices"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

var archivedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestBoardHasEveryColumnInOrderWithTheTasksAndTheirActivity(t *testing.T) {
	tasks := []*domain.Ticket{
		{ID: "t-done", Title: "Ship it", Status: domain.TicketStatusDone},
		{ID: "t-feed", Title: "Add SSE feed", Status: domain.TicketStatusInProgress},
		{ID: "t-port", Title: "Port tickets", Status: domain.TicketStatusInProgress},
		{ID: "t-old", Title: "Archived", Status: domain.TicketStatusDone, ArchivedAt: &archivedAt}, // off the board
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

func TestBoardListsEachColumnNewestStatusSinceFirst(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 19, h, 0, 0, 0, time.UTC) }
	tasks := []*domain.Ticket{
		{ID: "t-old", Title: "Oldest move", Status: domain.TicketStatusReview, StatusSince: at(8)},
		{ID: "t-new", Title: "Newest move", Status: domain.TicketStatusReview, StatusSince: at(11)},
		{ID: "t-mid", Title: "Middle move", Status: domain.TicketStatusReview, StatusSince: at(9)},
		{ID: "t-todo", Title: "Waiting", Status: domain.TicketStatusTodo, StatusSince: at(7)},
	}
	b := BuildBoard("p1", tasks, nil)

	var review []string
	for _, l := range b.Columns[3].Cards {
		review = append(review, l.TaskID)
	}
	if got := strings.Join(review, ","); got != "t-new,t-mid,t-old" {
		t.Fatalf("review column = %s, want t-new,t-mid,t-old", got)
	}
	if todo := b.Columns[1].Cards; len(todo) != 1 || todo[0].TaskID != "t-todo" {
		t.Errorf("todo column = %+v", todo)
	}
}

func TestBoardFeatureCardsMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/feature-cards.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Tasks []struct {
			ID, Title, Status string
			ParentID          string `json:"parent_id"`
			Since             int
			Reviews           int
			Merge             string
			Archived          bool
		} `json:"tasks"`
		Sessions []struct {
			TaskID string `json:"task_id"`
			Status string
		} `json:"sessions"`
		Columns map[string][]string `json:"columns"`
		Cards   []struct {
			ID, Bar, Summary, Tasks, Hint, Live, Reviews, Merge string
			Children, Archived                                  []string
		} `json:"cards"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	origin := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	var tasks []*domain.Ticket
	for _, tk := range fixture.Tasks {
		ticket := &domain.Ticket{ID: tk.ID, Title: tk.Title, Status: domain.TicketStatus(tk.Status), ParentID: tk.ParentID, StatusSince: origin.Add(time.Duration(tk.Since) * time.Minute), PendingReviews: tk.Reviews}
		if tk.Merge != "" {
			ticket.MergeState = &domain.MergeState{State: domain.MergeKind(tk.Merge), Merged: 1, Total: 1}
		}
		if tk.Archived {
			ticket.ArchivedAt = &archivedAt
		}
		tasks = append(tasks, ticket)
	}
	var sessions []*domain.Session
	for _, s := range fixture.Sessions {
		sessions = append(sessions, &domain.Session{ID: "s-" + s.TaskID, TicketID: s.TaskID, Status: domain.SessionStatus(s.Status)})
	}
	board := BuildBoard("p1", tasks, sessions)
	cards := map[string]Card{}
	for _, col := range board.Columns {
		var ids []string
		for _, card := range col.Cards {
			cards[card.TaskID] = card
			ids = append(ids, card.TaskID)
		}
		if got, want := strings.Join(ids, ","), strings.Join(fixture.Columns[col.Status], ","); got != want {
			t.Errorf("column %s = %s, want %s", col.Status, got, want)
		}
	}
	for _, want := range fixture.Cards {
		card, ok := cards[want.ID]
		if !ok {
			t.Errorf("no card for %s", want.ID)
			continue
		}
		if len(want.Children) == 0 {
			if card.Feature != nil {
				t.Errorf("card %s is a feature card, want a plain one", want.ID)
			}
			continue
		}
		f := card.Feature
		if f == nil {
			t.Errorf("card %s is plain, want a feature card", want.ID)
			continue
		}
		var kids, archived []string
		for _, c := range f.Children {
			kids = append(kids, c.TaskID)
			if c.Archived {
				archived = append(archived, c.TaskID)
			}
		}
		reviews := ""
		if card.Reviews > 0 {
			reviews = reviewsWord(card.Reviews)
		}
		merge := ""
		if card.Merge != nil {
			merge = card.Merge.Word
		}
		got := []string{f.Bar, f.Summary, f.Tasks, f.Hint, strings.Join(kids, ","), strings.Join(archived, ","), card.LiveState, reviews, merge}
		w := []string{want.Bar, want.Summary, want.Tasks, want.Hint, strings.Join(want.Children, ","), strings.Join(want.Archived, ","), want.Live, want.Reviews, want.Merge}
		if !slices.Equal(got, w) {
			t.Errorf("card %s:\n got %q\nwant %q", want.ID, got, w)
		}
	}
}

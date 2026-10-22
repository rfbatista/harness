package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

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
	if card.TaskID != "t-feed" || card.Href != "/projects/p1/tasks/t-feed" || card.LiveState() != "waiting" || card.LiveWord() != "waiting on you" {
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

func TestBoardFeatureCuesMatchTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/feature-cues.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ChildrenWords []struct {
			Count int    `json:"count"`
			Word  string `json:"word"`
		} `json:"children_words"`
		Tasks []struct {
			ID, Title, Status string
			ParentID          string `json:"parent_id"`
		} `json:"tasks"`
		Cards []struct {
			ID, Parent, Children string
		} `json:"cards"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, w := range fixture.ChildrenWords {
		if got := ChildrenWord(w.Count); got != w.Word {
			t.Errorf("ChildrenWord(%d) = %q, want %q", w.Count, got, w.Word)
		}
	}
	var tasks []*domain.Ticket
	for _, tk := range fixture.Tasks {
		tasks = append(tasks, &domain.Ticket{ID: tk.ID, Title: tk.Title, Status: domain.TicketStatus(tk.Status), ParentID: tk.ParentID})
	}
	cards := map[string]Card{}
	for _, c := range BuildBoard("p1", tasks, nil).Columns {
		for _, card := range c.Cards {
			cards[card.TaskID] = card
		}
	}
	if len(cards) != len(fixture.Cards) {
		t.Fatalf("%d cards, want %d", len(cards), len(fixture.Cards))
	}
	for _, want := range fixture.Cards {
		card := cards[want.ID]
		if card.Parent != want.Parent || ChildrenWord(card.Children) != want.Children {
			t.Errorf("card %s: parent %q children %q, want %q / %q", want.ID, card.Parent, ChildrenWord(card.Children), want.Parent, want.Children)
		}
	}
}

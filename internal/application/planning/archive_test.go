package planning_test

import (
	"context"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type archiveFixture struct {
	svc     *planning.Service
	history *sqlite.TaskStatusChangeRepository
	feed    *recorder
	pid     string
}

func newArchiveFixture(t *testing.T) *archiveFixture {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	f := &archiveFixture{history: sqlite.NewTaskStatusChangeRepository(db), feed: &recorder{}, pid: p.ID}
	f.svc = planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	f.svc.StatusHistory, f.svc.Announcer = f.history, f.feed
	f.svc.Comments = sqlite.NewTicketCommentRepository(db)
	return f
}

// doneTask creates a task and moves it to done, as the person does.
func (f *archiveFixture) doneTask(t *testing.T, title string) *domain.Ticket {
	t.Helper()
	ctx := context.Background()
	tk, err := f.svc.CreateTicket(ctx, f.pid, title, "", domain.TicketStatusReview, "")
	if err != nil {
		t.Fatal(err)
	}
	tk, err = f.svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusDone)})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func ids(tks []*domain.Ticket) []string {
	out := make([]string, 0, len(tks))
	for _, tk := range tks {
		out = append(out, tk.ID)
	}
	return out
}

// The person archives a done task and restores it, losing nothing: it comes
// back done, with its status history, documents and comments.
func TestADoneTaskArchivesAndRestoresLosingNothing(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	tk := f.doneTask(t, "Ship it")
	doc, err := f.svc.CreateDocument(f.pid, "Spec", "body", "", domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.LinkDocument(tk.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddTicketComment(ctx, tk.ID, "looks good"); err != nil {
		t.Fatal(err)
	}
	moves := len(f.history.ListByTask(tk.ID))

	archived, err := f.svc.ArchiveTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil || archived.Status != domain.TicketStatusDone {
		t.Fatalf("archived = %+v", archived)
	}
	got, err := f.svc.GetTicket(ctx, tk.ID)
	if err != nil || got.ArchivedAt == nil {
		t.Fatalf("a session still reads the archived task: %+v, %v", got, err)
	}

	restored, err := f.svc.RestoreTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ArchivedAt != nil || restored.Status != domain.TicketStatusDone {
		t.Fatalf("restored = %+v", restored)
	}
	if n := len(f.history.ListByTask(tk.ID)); n != moves {
		t.Errorf("status history has %d moves, want %d", n, moves)
	}
	if docs := f.svc.ListTicketDocuments(ctx, tk.ID); len(docs) != 1 || docs[0].ID != doc.ID {
		t.Errorf("documents = %+v", docs)
	}
	if cs, _ := f.svc.ListTicketComments(ctx, tk.ID, time.Time{}); len(cs) != 1 || cs[0].Body != "looks good" {
		t.Errorf("comments = %+v", cs)
	}
}

// Archiving an archived task and restoring an unarchived one change nothing
// and answer the task.
func TestArchiveAndRestoreAreNoOpsTheSecondTime(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	tk := f.doneTask(t, "Ship it")

	if again, err := f.svc.RestoreTicket(ctx, tk.ID); err != nil || again.ArchivedAt != nil || again.ID != tk.ID {
		t.Fatalf("restoring an unarchived task = %+v, %v", again, err)
	}
	first, err := f.svc.ArchiveTicket(ctx, tk.ID)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.svc.ArchiveTicket(ctx, tk.ID)
	if err != nil || again.ArchivedAt == nil || !again.ArchivedAt.Equal(*first.ArchivedAt) {
		t.Fatalf("archiving again = %+v, %v; first archived at %v", again, err, first.ArchivedAt)
	}
	if _, err := f.svc.ArchiveTicket(ctx, "nope"); code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("unknown task: %v", err)
	}
	if _, err := f.svc.RestoreTicket(ctx, "nope"); code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("unknown task: %v", err)
	}
}

// Only a done task archives; an archived task keeps its status until it is
// restored, but its text can still change and it can still be deleted.
func TestAnArchivedTaskKeepsItsStatusUntilRestored(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	inReview, err := f.svc.CreateTicket(ctx, f.pid, "Waiting", "", domain.TicketStatusReview, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ArchiveTicket(ctx, inReview.ID); code(err) != "TASK_NOT_DONE" {
		t.Fatalf("archiving a task in review: %v", err)
	}

	tk := f.doneTask(t, "Ship it")
	if _, err := f.svc.ArchiveTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusTodo)}); code(err) != "TASK_ARCHIVED" {
		t.Fatalf("moving an archived task: %v", err)
	}
	if _, err := f.svc.UpdateTicket(ctx, tk.ID, "Ship it", "", domain.TicketStatusInProgress); code(err) != "TASK_ARCHIVED" {
		t.Fatalf("moving an archived task through UpdateTicket: %v", err)
	}
	renamed, err := f.svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Title: strp("Shipped"), Status: statusp(domain.TicketStatusDone)})
	if err != nil || renamed.Title != "Shipped" || renamed.ArchivedAt == nil {
		t.Fatalf("renaming an archived task = %+v, %v", renamed, err)
	}

	if _, err := f.svc.RestoreTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if moved, err := f.svc.PatchTicket(ctx, tk.ID, ports.TicketPatch{Status: statusp(domain.TicketStatusTodo)}); err != nil || moved.Status != domain.TicketStatusTodo {
		t.Fatalf("moving a restored task = %+v, %v", moved, err)
	}

	gone := f.doneTask(t, "Old")
	if _, err := f.svc.ArchiveTicket(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteTicket(ctx, gone.ID); err != nil {
		t.Fatalf("deleting an archived task: %v", err)
	}
	if _, err := f.svc.GetTicket(ctx, gone.ID); code(err) != "TICKET_NOT_FOUND" {
		t.Fatalf("the deleted task is still there: %v", err)
	}
}

// Archived tasks leave the default list; the archived list has only them,
// newest archived first.
func TestArchivedTasksLeaveTheListAndHaveTheirOwn(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	open, err := f.svc.CreateTicket(ctx, f.pid, "Open", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	first := f.doneTask(t, "First")
	second := f.doneTask(t, "Second")
	kept := f.doneTask(t, "Kept")
	if _, err := f.svc.ArchiveTicket(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := f.svc.ArchiveTicket(ctx, second.ID); err != nil {
		t.Fatal(err)
	}

	list, err := f.svc.ListTickets(ctx, f.pid, ports.TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(list); len(got) != 2 || !(got[0] == open.ID && got[1] == kept.ID || got[0] == kept.ID && got[1] == open.ID) {
		t.Fatalf("default list = %v, want only %s and %s", got, open.ID, kept.ID)
	}
	archived, err := f.svc.ListArchivedTickets(ctx, f.pid)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(archived); len(got) != 2 || got[0] != second.ID || got[1] != first.ID {
		t.Fatalf("archived list = %v, want %s then %s", got, second.ID, first.ID)
	}
}

// Archive and restore each put the task on the feed once, carrying its new
// archived_at.
func TestArchiveAndRestoreAreAnnounced(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	tk := f.doneTask(t, "Ship it")
	before := len(f.feed.all())

	if _, err := f.svc.ArchiveTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.RestoreTicket(ctx, tk.ID); err != nil {
		t.Fatal(err)
	}
	got := f.feed.all()[before:]
	if len(got) != 2 {
		t.Fatalf("announced %d changes, want archive and restore: %+v", len(got), got)
	}
	if got[0].deleted || got[0].ticket.ID != tk.ID || got[0].ticket.ArchivedAt == nil {
		t.Errorf("archive announced as %+v", got[0])
	}
	if got[1].deleted || got[1].ticket.ID != tk.ID || got[1].ticket.ArchivedAt != nil {
		t.Errorf("restore announced as %+v", got[1])
	}
}

// Archive all takes exactly the done tasks that are still on the board off
// it, answers which, and announces each once.
func TestArchiveAllDoneArchivesOnlyTheDoneTasksOnTheBoard(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	first := f.doneTask(t, "First")
	second := f.doneTask(t, "Second")
	already := f.doneTask(t, "Already")
	if _, err := f.svc.ArchiveTicket(ctx, already.ID); err != nil {
		t.Fatal(err)
	}
	inReview, err := f.svc.CreateTicket(ctx, f.pid, "Waiting", "", domain.TicketStatusReview, "")
	if err != nil {
		t.Fatal(err)
	}
	before := len(f.feed.all())

	got, err := f.svc.ArchiveDoneTickets(ctx, f.pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !(got[0] == first.ID && got[1] == second.ID || got[0] == second.ID && got[1] == first.ID) {
		t.Fatalf("archived %v, want %s and %s", got, first.ID, second.ID)
	}
	announced := f.feed.all()[before:]
	if len(announced) != 2 {
		t.Fatalf("announced %d changes, want one per archived task: %+v", len(announced), announced)
	}
	for _, a := range announced {
		if a.deleted || a.ticket.ArchivedAt == nil || (a.ticket.ID != first.ID && a.ticket.ID != second.ID) {
			t.Errorf("announced %+v", a)
		}
	}
	list, err := f.svc.ListTickets(ctx, f.pid, ports.TicketFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if left := ids(list); len(left) != 1 || left[0] != inReview.ID {
		t.Fatalf("board = %v, want only %s", left, inReview.ID)
	}
}

// On a project with nothing done, archive all archives nothing and says so
// with an empty list, not a missing one.
func TestArchiveAllDoneOnAProjectWithNothingDone(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateTicket(ctx, f.pid, "Waiting", "", domain.TicketStatusReview, ""); err != nil {
		t.Fatal(err)
	}
	before := len(f.feed.all())
	got, err := f.svc.ArchiveDoneTickets(ctx, f.pid)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("archive all = %#v, %v; want an empty list", got, err)
	}
	if n := len(f.feed.all()) - before; n != 0 {
		t.Errorf("announced %d changes, want none", n)
	}
	if _, err := f.svc.ArchiveDoneTickets(ctx, "nope"); code(err) != "PROJECT_NOT_FOUND" {
		t.Fatalf("unknown project: %v", err)
	}
}

// The archived count follows archiving and restoring; the open count never
// moves, since archived tasks are done.
func TestTheArchivedCountFollowsArchiveAndRestore(t *testing.T) {
	f := newArchiveFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateTicket(ctx, f.pid, "Open", "", domain.TicketStatusTodo, ""); err != nil {
		t.Fatal(err)
	}
	first := f.doneTask(t, "First")
	f.doneTask(t, "Second")
	counts := func() (archived, open int) {
		t.Helper()
		n, err := f.svc.ArchivedTaskCount(ctx, f.pid)
		if err != nil {
			t.Fatal(err)
		}
		activity, err := f.svc.TaskActivityByProject(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if a := activity[f.pid]; a.ArchivedCount != n {
			t.Fatalf("project activity counts %d archived, the project page %d", a.ArchivedCount, n)
		}
		return n, activity[f.pid].OpenCount
	}
	if a, o := counts(); a != 0 || o != 1 {
		t.Fatalf("before archiving: archived %d open %d, want 0 and 1", a, o)
	}
	if _, err := f.svc.ArchiveDoneTickets(ctx, f.pid); err != nil {
		t.Fatal(err)
	}
	if a, o := counts(); a != 2 || o != 1 {
		t.Fatalf("after archiving all: archived %d open %d, want 2 and 1", a, o)
	}
	if _, err := f.svc.RestoreTicket(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if a, o := counts(); a != 1 || o != 1 {
		t.Fatalf("after restoring one: archived %d open %d, want 1 and 1", a, o)
	}
	if n, err := f.svc.ArchivedTaskCount(ctx, "empty"); err != nil || n != 0 {
		t.Fatalf("a project without tasks: %d, %v", n, err)
	}
}

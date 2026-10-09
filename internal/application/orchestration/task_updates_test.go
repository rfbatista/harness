//go:build !windows

package orchestration

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func TestTaskUpdateText(t *testing.T) {
	file := domain.TicketResource{ID: "r1", Kind: domain.ResourceImage, Title: "Login screen"}
	if got, want := resourceUpdateText(file), `[task update · resource added] The person added an image to this task: "Login screen" (resource_id r1). Read it with read_task_resource if it bears on your work.`; got != want {
		t.Fatalf("file:\n%s\nwant\n%s", got, want)
	}
	link := domain.TicketResource{ID: "r2", Kind: domain.ResourceLink, Title: "Spec", URL: "https://x.io/spec"}
	if got := resourceUpdateText(link); !strings.Contains(got, `"Spec" (resource_id r2, url https://x.io/spec).`) {
		t.Fatalf("link: %s", got)
	}

	c := domain.TicketComment{ID: "c1", Body: "line one\nline two"}
	if got, want := commentUpdateText(c), "[task update · comment] The person commented on this task (comment_id c1):\nline one\nline two"; got != want {
		t.Fatalf("comment:\n%q\nwant\n%q", got, want)
	}
	long := domain.TicketComment{ID: "c2", Body: strings.Repeat("é", 2500)}
	got := commentUpdateText(long)
	body := strings.SplitN(got, "\n", 2)[1]
	if !strings.HasSuffix(body, "…\n(cut; read the whole comment with list_task_comments)") {
		t.Fatalf("cut comment ends %q", body[len(body)-80:])
	}
	if n := utf8.RuneCountInString(strings.SplitN(body, "…", 2)[0]); n != 2000 || !utf8.ValidString(got) {
		t.Fatalf("kept %d runes, valid %v", n, utf8.ValidString(got))
	}
	exact := domain.TicketComment{ID: "c3", Body: strings.Repeat("x", 2000)}
	if strings.Contains(commentUpdateText(exact), "cut;") {
		t.Fatal("2 000 characters are not cut")
	}
}

// recordingDelivery stands in for the courier.
type recordingDelivery struct {
	mu    sync.Mutex
	got   []string // "session|key|text"
	block chan struct{}
}

func (d *recordingDelivery) deliver(id, key, text string) error {
	if d.block != nil {
		<-d.block
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if id == "gone" {
		return &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	d.got = append(d.got, id+"|"+key+"|"+text)
	return nil
}

func (d *recordingDelivery) list() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.got...)
}

func newUpdatesFixture(t *testing.T) (*Service, *eventbus.Bus, *recordingDelivery) {
	t.Helper()
	svc, _ := newInteractiveService(t)
	for _, s := range []*domain.Session{
		{ID: "arch", ProjectID: "p1", TicketID: "tk1", Status: domain.SessionRunning},
		{ID: "dlg", ProjectID: "p1", TicketID: "tk1", Status: domain.SessionIdle},
		{ID: "done", ProjectID: "p1", TicketID: "tk1", Status: domain.SessionDone},
		{ID: "elsewhere", ProjectID: "p1", TicketID: "tk2", Status: domain.SessionRunning},
	} {
		if _, err := svc.sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	d := &recordingDelivery{}
	svc.deliverUpdate = d.deliver
	bus := eventbus.New()
	svc.SubscribeTaskUpdates(bus)
	return svc, bus, d
}

// An addition goes to every running session of its task, under the item's
// key, in the order the person made them; stopped sessions and other tasks'
// get nothing.
func TestTaskUpdates_DeliveredToTheTasksRunningSessions(t *testing.T) {
	_, bus, d := newUpdatesFixture(t)
	ctx := context.Background()
	_ = bus.Publish(ctx, domain.TicketResourceAdded{Resource: domain.TicketResource{ID: "r1", TicketID: "tk1", Kind: domain.ResourceFile, Title: "notes.md"}})
	_ = bus.Publish(ctx, domain.TicketCommentAdded{Comment: domain.TicketComment{ID: "c1", TicketID: "tk1", Body: "see the notes"}})

	eventually(t, "four deliveries", func() bool { return len(d.list()) == 4 })
	got := d.list()
	var keys []string
	for _, g := range got {
		parts := strings.SplitN(g, "|", 3)
		keys = append(keys, parts[0]+" "+parts[1])
	}
	// Items in order; within one item the sessions' order does not matter.
	slices.Sort(keys[:2])
	slices.Sort(keys[2:])
	if strings.Join(keys, ",") != "arch task-update:r1,dlg task-update:r1,arch task-update:c1,dlg task-update:c1" {
		t.Fatalf("deliveries = %v", keys)
	}
	if !strings.HasPrefix(strings.SplitN(got[0], "|", 3)[2], "[task update · resource added]") ||
		!strings.HasPrefix(strings.SplitN(got[2], "|", 3)[2], "[task update · comment]") {
		t.Fatalf("texts = %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if len(d.list()) != 4 {
		t.Fatalf("more deliveries than sessions: %v", d.list())
	}
}

// A session's comment wakes no session, the writer included, so agents do
// not answer each other in a loop; the person's next comment still does.
func TestTaskUpdates_ASessionsCommentSendsNoTurn(t *testing.T) {
	_, bus, d := newUpdatesFixture(t)
	ctx := context.Background()
	_ = bus.Publish(ctx, domain.TicketCommentAdded{Comment: domain.TicketComment{ID: "c1", TicketID: "tk1", Author: domain.CommentBySession, SessionID: "dlg", Body: "done"}})
	_ = bus.Publish(ctx, domain.TicketCommentAdded{Comment: domain.TicketComment{ID: "c2", TicketID: "tk1", Author: domain.CommentByPerson, Body: "thanks"}})

	eventually(t, "the person's comment delivered", func() bool { return len(d.list()) == 2 })
	time.Sleep(50 * time.Millisecond)
	for _, g := range d.list() {
		if strings.Contains(g, "task-update:c1") {
			t.Fatalf("a session's comment was delivered: %v", d.list())
		}
	}
	if len(d.list()) != 2 {
		t.Fatalf("deliveries = %v", d.list())
	}
}

// Publishing never waits on a session: the event returns while delivery is
// stuck.
func TestTaskUpdates_PublishDoesNotWaitOnDelivery(t *testing.T) {
	_, bus, d := newUpdatesFixture(t)
	d.block = make(chan struct{})
	done := make(chan struct{})
	go func() {
		for i := range 5 {
			_ = bus.Publish(context.Background(), domain.TicketCommentAdded{Comment: domain.TicketComment{ID: fmt.Sprint("c", i), TicketID: "tk1", Body: "x"}})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish waited on delivery")
	}
	close(d.block)
	eventually(t, "every delivery", func() bool { return len(d.list()) == 10 })
	if got := d.list(); !strings.Contains(got[0], "task-update:c0") || !strings.Contains(got[9], "task-update:c4") {
		t.Fatalf("order = %v", got)
	}
}

// Through the real courier: a client-run session busy in a turn gets the
// update from its Stop hook, never cutting in.
func TestTaskUpdates_ThroughTheCourier(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	bus := eventbus.New()
	svc.SubscribeTaskUpdates(bus)
	_ = bus.Publish(context.Background(), domain.TicketCommentAdded{Comment: domain.TicketComment{ID: "c1", TicketID: sess.TicketID, Body: "please also check X"}})
	var next string
	eventually(t, "the update queued", func() bool {
		svc.courier.mu.Lock()
		defer svc.courier.mu.Unlock()
		return len(svc.courier.outbox[sess.ID]) == 1
	})
	next, err := svc.TurnEnded(context.Background(), sess.ID, false)
	if err != nil || next != "[task update · comment] The person commented on this task (comment_id c1):\nplease also check X" {
		t.Fatalf("Stop hook = %q, %v", next, err)
	}
}

// fakeTaskInputs counts what a task has.
type fakeTaskInputs struct {
	resources, comments int
	err                 error
}

func (f fakeTaskInputs) ListTicketResources(context.Context, string) ([]*domain.TicketResource, error) {
	return make([]*domain.TicketResource, f.resources), f.err
}

func (f fakeTaskInputs) ListTicketComments(context.Context, string, time.Time) ([]*domain.TicketComment, error) {
	return make([]*domain.TicketComment, f.comments), f.err
}

var (
	_ ports.TaskResourceReader = fakeTaskInputs{}
	_ ports.TaskCommentReader  = fakeTaskInputs{}
)

// Every task session's brief has the section, with the counts when they are
// known, and names the three read tools.
func TestStartInteractive_BriefsTheResourcesAndComments(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Resources, svc.Comments = fakeTaskInputs{resources: 2, comments: 1}, fakeTaskInputs{resources: 2, comments: 1}
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	brief := briefOf(t, launch)
	for _, want := range []string{
		"## Resources and comments\n",
		"This task has 2 resources and 1 comment.",
		"[task update · …]",
		"\n- mcp__task__list_task_resources — ",
		"\n- mcp__task__read_task_resource — ",
		"\n- mcp__task__list_task_comments — ",
		"\n- mcp__task__add_task_comment — ",
		"\n- mcp__task__update_task_comment — ",
		"\n- mcp__task__delete_task_comment — ",
		"message_architect",
		"your comment wakes no session",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief lacks %q:\n%s", want, brief)
		}
	}

	svc.Resources, svc.Comments = fakeTaskInputs{err: fmt.Errorf("db down")}, fakeTaskInputs{err: fmt.Errorf("db down")}
	_, launch = startInteractive(t, svc, InteractiveRequest{})
	brief = briefOf(t, launch)
	if !strings.Contains(brief, "## Resources and comments\n") || strings.Contains(brief, "This task has") {
		t.Fatalf("a failed count keeps the section, without counts:\n%s", brief)
	}

	svc.Resources, svc.Comments = nil, nil
	_, launch = startInteractive(t, svc, InteractiveRequest{})
	if !strings.Contains(briefOf(t, launch), "mcp__task__list_task_comments") {
		t.Fatal("the section is there even without readers")
	}
}

func TestResourcesSectionCounts(t *testing.T) {
	for _, c := range []struct {
		r, c int
		want string
	}{
		{0, 0, "This task has no resources and no comments yet."},
		{1, 0, "This task has 1 resource and no comments."},
		{3, 5, "This task has 3 resources and 5 comments."},
	} {
		if got := resourcesSection(taskCounts{c.r, c.c, true}); !strings.Contains(got, c.want) {
			t.Errorf("%d/%d: lacks %q:\n%s", c.r, c.c, c.want, got)
		}
	}
}

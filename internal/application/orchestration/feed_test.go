package orchestration

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func follow(t *testing.T, svc *Service, projectID string) <-chan ports.SessionChange {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, err := svc.FollowProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func next(t *testing.T, c <-chan ports.SessionChange) ports.SessionChange {
	t.Helper()
	select {
	case ch, ok := <-c:
		if !ok {
			t.Fatal("feed closed")
		}
		return ch
	case <-time.After(5 * time.Second):
		t.Fatal("no change")
	}
	return ports.SessionChange{}
}

func TestFeed_OnlyTheProjectsChanges(t *testing.T) {
	svc, _ := newInteractiveService(t)
	other := follow(t, svc, "another-project")
	mine := follow(t, svc, "p1")

	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if got := next(t, mine); got.Session.ID != sess.ID || got.Session.Status != domain.SessionRunning {
		t.Fatalf("change = %+v, want %s running", got.Session, sess.ID)
	}
	select {
	case c := <-other:
		t.Fatalf("another project's follower got %+v", c.Session)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestFeed_EndAndDelete(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	changes := follow(t, svc, "p1")

	if _, err := svc.EndInteractive(context.Background(), sess.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	if got := next(t, changes); got.Session.Status != domain.SessionFailed {
		t.Fatalf("after end: %+v, want failed", got.Session)
	}
	if err := svc.Delete(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := next(t, changes); !got.Deleted || got.Session.ID != sess.ID {
		t.Fatalf("after delete: %+v, want the session deleted", got)
	}
}

// Output is the conversation, not the record; it is not worth a change.
func TestFeed_QuietEventsAreNotChanges(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	changes := follow(t, svc, "p1")
	svc.publish(sess.ID, SessionEvent{Type: "output", Text: "hello"})
	select {
	case c := <-changes:
		t.Fatalf("output produced a change: %+v", c.Session)
	case <-time.After(100 * time.Millisecond):
	}
}

// A follower that falls behind is dropped, so it knows to reload.
func TestFeed_SlowFollowerIsDropped(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	changes := follow(t, svc, "p1")
	for range feedBuffer + 1 {
		svc.publish(sess.ID, SessionEvent{Type: "status", Text: "busy"})
	}
	n := 0
	for range changes {
		n++
	}
	if n != feedBuffer {
		t.Fatalf("read %d changes before the close, want the %d buffered", n, feedBuffer)
	}
}

// A session an agent starts (start_task_session) reaches followers as it is
// created, naming the session that started it.
func TestFeed_ASessionStartedByAnotherCarriesItsParent(t *testing.T) {
	svc, _ := newInteractiveService(t)
	mine := follow(t, svc, "p1")
	sess, _ := startInteractive(t, svc, InteractiveRequest{ParentSessionID: "parent-1"})
	got := next(t, mine)
	if got.Session.ID != sess.ID || got.Session.ParentSessionID != "parent-1" {
		t.Fatalf("change = %+v", got.Session)
	}
	if stored := sessionOf(svc, sess.ID); stored.ParentSessionID != "parent-1" {
		t.Fatalf("stored parent = %q", stored.ParentSessionID)
	}
}

var _ ports.TicketAnnouncer = (*Service)(nil)

// A ticket change reaches the project's followers as a ticket message, with
// no session on it, and never another project's followers.
func TestFeed_TicketChangesReachTheProjectsFollowers(t *testing.T) {
	svc, _ := newInteractiveService(t)
	other := follow(t, svc, "another-project")
	mine := follow(t, svc, "p1")

	tk := &domain.Ticket{ID: "tk1", ProjectID: "p1", Title: "Ship the thing", Status: domain.TicketStatusReview}
	svc.AnnounceTicket(tk, false)
	got := next(t, mine)
	if got.Session != nil || got.Ticket == nil || got.Ticket.ID != "tk1" || got.Ticket.Status != domain.TicketStatusReview || got.Deleted {
		t.Fatalf("change = %+v", got)
	}

	svc.AnnounceTicket(tk, true)
	if got := next(t, mine); !got.Deleted || got.Ticket == nil || got.Ticket.ID != "tk1" {
		t.Fatalf("after delete: %+v", got)
	}
	select {
	case c := <-other:
		t.Fatalf("another project's follower got %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
}

// Ticket and session messages share one stream, in the order they happened.
func TestFeed_TicketAndSessionChangesInterleaveInOrder(t *testing.T) {
	svc, _ := newInteractiveService(t)
	mine := follow(t, svc, "p1")
	svc.AnnounceTicket(&domain.Ticket{ID: "tk1", ProjectID: "p1", Status: domain.TicketStatusTodo}, false)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	svc.AnnounceTicket(&domain.Ticket{ID: "tk1", ProjectID: "p1", Status: domain.TicketStatusInProgress}, false)

	if got := next(t, mine); got.Ticket == nil || got.Ticket.Status != domain.TicketStatusTodo {
		t.Fatalf("1st = %+v", got)
	}
	if got := next(t, mine); got.Session == nil || got.Session.ID != sess.ID {
		t.Fatalf("2nd = %+v", got)
	}
	if got := next(t, mine); got.Ticket == nil || got.Ticket.Status != domain.TicketStatusInProgress {
		t.Fatalf("3rd = %+v", got)
	}
}

// Every open page learns a session came back, and when it can be resumed
// again, from the project feed alone.
func TestFeed_ResumeAndEndCarryResumability(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	changes := follow(t, svc, "p1")

	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	got := next(t, changes).Session
	if got.ID != sess.ID || got.Status != domain.SessionRunning || got.Resumable || got.ResumeBlocked != "SESSION_ALREADY_RUNNING" {
		t.Fatalf("after resume = %+v", got)
	}

	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	got = next(t, changes).Session
	if got.Status != domain.SessionDone || !got.Resumable || got.ResumeBlocked != "" {
		t.Fatalf("after end = %+v", got)
	}
}

// followerCounts records what the feed reports of its followers.
type followerCounts struct {
	mu     sync.Mutex
	counts map[string][]int
}

func (o *followerCounts) ProjectFollowers(projectID string, followers int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.counts[projectID] = append(o.counts[projectID], followers)
}

func (o *followerCounts) of(projectID string) []int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.counts[projectID])
}

// waitCounts waits for the reported counts of projectID, since a follower
// leaves once its context's end is noticed.
func (o *followerCounts) waitCounts(t *testing.T, projectID string, want ...int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(o.of(projectID), want) {
		if time.Now().After(deadline) {
			t.Fatalf("%s followers reported %v, want %v", projectID, o.of(projectID), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// The feed tells its observer how many follow each project as followers come
// and go, whether they leave or are dropped for falling behind.
func TestFeed_ReportsEachProjectsFollowers(t *testing.T) {
	svc, _ := newInteractiveService(t)
	seen := &followerCounts{counts: map[string][]int{}}
	svc.ObserveFeedFollowers(seen)

	ctx1, leave1 := context.WithCancel(context.Background())
	defer leave1()
	if _, err := svc.FollowProject(ctx1, "p1"); err != nil {
		t.Fatal(err)
	}
	ctx2, leave2 := context.WithCancel(context.Background())
	defer leave2()
	if _, err := svc.FollowProject(ctx2, "p1"); err != nil {
		t.Fatal(err)
	}
	follow(t, svc, "p2")
	seen.waitCounts(t, "p1", 1, 2)
	seen.waitCounts(t, "p2", 1)

	leave1()
	seen.waitCounts(t, "p1", 1, 2, 1)
	leave2()
	seen.waitCounts(t, "p1", 1, 2, 1, 0)

	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	slow := follow(t, svc, "p1")
	for range feedBuffer + 1 {
		svc.publish(sess.ID, SessionEvent{Type: "status", Text: "busy"})
	}
	for range slow {
	}
	seen.waitCounts(t, "p1", 1, 2, 1, 0, 1, 0)
}

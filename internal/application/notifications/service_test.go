package notifications_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/notifications/notificationstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var ctx = context.Background()

type rig struct {
	*notificationstest.Env
	svc *notifications.Service
	bus *eventbus.Bus
}

func newRig(t *testing.T) *rig {
	t.Helper()
	e := notificationstest.NewEnv(t)
	svc := e.Service(t, notifications.Options{})
	bus := eventbus.New()
	svc.Subscribe(bus)
	return &rig{Env: e, svc: svc, bus: bus}
}

func (r *rig) publish(t *testing.T, ev domain.Event) {
	t.Helper()
	if err := r.bus.Publish(ctx, ev); err != nil {
		t.Fatalf("publish %s: %v", ev.EventName(), err)
	}
}

func (r *rig) list(t *testing.T, q ports.NotificationQuery) *ports.NotificationPage {
	t.Helper()
	page, err := r.svc.ListNotifications(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func (r *rig) task(t *testing.T, projectID, title string) *domain.Ticket {
	t.Helper()
	tk, err := r.Tickets.Create(projectID, title, "", domain.TicketStatusTodo)
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

func (r *rig) session(t *testing.T, id, projectID, taskID string) {
	t.Helper()
	if _, err := r.Sessions.Create(&domain.Session{ID: id, ProjectID: projectID, TicketID: taskID, Status: domain.SessionRunning}); err != nil {
		t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := errs.Code(err); got != code {
		t.Fatalf("error code = %q (err %v), want %q", got, err, code)
	}
}

func TestAReviewRequestIsRecordedOnceAndResolvedWhenAnswered(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	open := domain.ReviewRequestChanged{ReviewID: "rv1", TaskID: tk.ID, ProjectID: r.ProjectA, Subject: "the plan", AboutSessionID: "s1", State: domain.ReviewPending}
	r.publish(t, open)
	r.publish(t, open)

	page := r.list(t, ports.NotificationQuery{})
	if len(page.Notifications) != 1 || page.UnreadCount != 1 {
		t.Fatalf("after the same review twice = %d notifications, %d unread; want 1, 1", len(page.Notifications), page.UnreadCount)
	}
	n := page.Notifications[0]
	if n.Kind != domain.NotificationReviewRequested || n.Level != domain.NotificationAction || n.Source != domain.NotificationFromSystem ||
		n.Title != "Review requested: the plan" || n.SessionID != "s1" || n.TaskTitle != "Ship" || n.ProjectName != "Alpha" ||
		n.Link != domain.TaskLink(r.ProjectA, tk.ID) {
		t.Fatalf("review notification = %+v", n)
	}

	answered := open
	answered.State = domain.ReviewApproved
	r.publish(t, answered)
	page = r.list(t, ports.NotificationQuery{})
	if page.UnreadCount != 0 || page.Notifications[0].ResolvedAt == nil || page.Notifications[0].ReadAt != nil {
		t.Fatalf("after the answer = %+v (unread %d), want it resolved, not read", page.Notifications[0], page.UnreadCount)
	}
}

func TestASessionWaitingIsResolvedWhenItMovesOnAndRecordedAgainWhenItWaitsAgain(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	r.session(t, "s1", r.ProjectA, tk.ID)
	waiting := domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval}

	r.publish(t, waiting)
	r.publish(t, waiting)
	if page := r.list(t, ports.NotificationQuery{}); len(page.Notifications) != 1 || page.UnreadCount != 1 ||
		page.Notifications[0].Kind != domain.NotificationSessionWaiting || page.Notifications[0].SessionID != "s1" ||
		page.Notifications[0].TaskID != tk.ID {
		t.Fatalf("waiting twice = %+v", page)
	}
	r.publish(t, domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionThinking})
	if page := r.list(t, ports.NotificationQuery{}); page.UnreadCount != 0 {
		t.Fatalf("after moving on, unread = %d", page.UnreadCount)
	}
	r.publish(t, waiting)
	if page := r.list(t, ports.NotificationQuery{}); len(page.Notifications) != 2 || page.UnreadCount != 1 {
		t.Fatalf("waiting again = %d notifications, %d unread; want 2, 1", len(page.Notifications), page.UnreadCount)
	}
	r.publish(t, domain.SessionDeleted{SessionID: "s1"})
	if page := r.list(t, ports.NotificationQuery{}); page.UnreadCount != 0 {
		t.Fatalf("after the session is deleted, unread = %d", page.UnreadCount)
	}
}

func TestAFailedSessionIsRecordedOnceAndResolvesItsWait(t *testing.T) {
	r := newRig(t)
	r.session(t, "s1", r.ProjectA, "")
	r.publish(t, domain.SessionStatusChanged{SessionID: "s1", Status: domain.SessionWaitingApproval})
	r.publish(t, domain.SessionEnded{SessionID: "s1", Status: domain.SessionFailed})
	r.publish(t, domain.SessionEnded{SessionID: "s1", Status: domain.SessionFailed})

	page := r.list(t, ports.NotificationQuery{UnreadOnly: true})
	if len(page.Notifications) != 1 || page.Notifications[0].Kind != domain.NotificationSessionFailed ||
		page.Notifications[0].Level != domain.NotificationInfo || page.Notifications[0].Link != domain.ProjectLink(r.ProjectA) {
		t.Fatalf("unread after failing twice = %+v", page.Notifications)
	}
	// Read, it is still never recorded again.
	if _, err := r.svc.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{All: true}); err != nil {
		t.Fatal(err)
	}
	r.publish(t, domain.SessionEnded{SessionID: "s1", Status: domain.SessionFailed})
	if page := r.list(t, ports.NotificationQuery{}); len(page.Notifications) != 2 {
		t.Fatalf("failing after a read = %d notifications, want the wait and the failure only", len(page.Notifications))
	}
	r.publish(t, domain.SessionEnded{SessionID: "s2", Status: domain.SessionDone})
}

func TestATaskInReviewIsResolvedWhenItLeavesReview(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	toReview := domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tk.ID, ProjectID: r.ProjectA, Status: domain.TicketStatusReview, Reason: "  ready  "}}
	r.publish(t, toReview)
	r.publish(t, toReview)
	page := r.list(t, ports.NotificationQuery{})
	if len(page.Notifications) != 1 || page.Notifications[0].Kind != domain.NotificationTaskReview || page.Notifications[0].Body != "ready" {
		t.Fatalf("task to review twice = %+v", page.Notifications)
	}
	r.publish(t, domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tk.ID, ProjectID: r.ProjectA, Status: domain.TicketStatusDone}})
	if page := r.list(t, ports.NotificationQuery{}); page.UnreadCount != 0 {
		t.Fatalf("after leaving review, unread = %d", page.UnreadCount)
	}
}

func TestDeletingATaskResolvesItsSystemNotificationsAndKeepsAgentOnes(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	r.publish(t, domain.TicketStatusChanged{Change: domain.TaskStatusChange{TaskID: tk.ID, ProjectID: r.ProjectA, Status: domain.TicketStatusReview}})
	r.publish(t, domain.ReviewRequestChanged{ReviewID: "rv1", TaskID: tk.ID, ProjectID: r.ProjectA, Subject: "x", State: domain.ReviewPending})
	agent, err := r.svc.Notify(ctx, ports.NotifyRequest{SessionID: "s1", ProjectID: r.ProjectA, TaskID: tk.ID, Title: "heads up"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tickets.Delete(tk.ID); err != nil {
		t.Fatal(err)
	}
	r.publish(t, domain.TicketDeleted{TicketID: tk.ID, ProjectID: r.ProjectA})

	page := r.list(t, ports.NotificationQuery{UnreadOnly: true})
	if len(page.Notifications) != 1 || page.Notifications[0].ID != agent.ID {
		t.Fatalf("unread after deleting the task = %+v, want only the agent's", page.Notifications)
	}
	if n := page.Notifications[0]; n.TaskID != tk.ID || n.TaskTitle != "" {
		t.Fatalf("a deleted task's notification = %+v, want its task id and no title", n)
	}
}

func TestDeletingAProjectTakesItsNotificationsAndStreamsTheCount(t *testing.T) {
	r := newRig(t)
	r.publish(t, domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: r.ProjectA, Subject: "a", State: domain.ReviewPending})
	r.publish(t, domain.ReviewRequestChanged{ReviewID: "rv2", ProjectID: r.ProjectB, Subject: "b", State: domain.ReviewPending})
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	changes, _ := r.svc.FollowNotifications(fctx)

	r.publish(t, domain.ProjectDeleted{ProjectID: r.ProjectA})
	select {
	case c := <-changes:
		if c.Changed == nil || len(c.Changed) != 0 || c.UnreadCount != 1 {
			t.Fatalf("project delete change = %#v, want an empty change and 1 unread", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change for the project delete")
	}
	if page := r.list(t, ports.NotificationQuery{}); len(page.Notifications) != 1 || page.Notifications[0].ProjectID != r.ProjectB {
		t.Fatalf("after the delete = %+v", page.Notifications)
	}
}

func TestNamesAreReadWhenTheInboxIsRead(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	if _, err := r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, TaskID: tk.ID, Title: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Projects.Update(r.ProjectA, "Renamed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Tickets.Update(tk.ID, "Ship it", "", tk.Status); err != nil {
		t.Fatal(err)
	}
	n := r.list(t, ports.NotificationQuery{}).Notifications[0]
	if n.ProjectName != "Renamed" || n.TaskTitle != "Ship it" {
		t.Fatalf("names = %q / %q, want the current ones", n.ProjectName, n.TaskTitle)
	}
}

// failingRepo fails every write, inside a transaction too.
type failingRepo struct{ ports.NotificationRepository }

func (r failingRepo) Transaction(fn func(tx ports.NotificationRepository) error) error {
	return r.NotificationRepository.Transaction(func(tx ports.NotificationRepository) error {
		return fn(failingRepo{tx})
	})
}

func (failingRepo) Create(*domain.Notification) (*domain.Notification, error) {
	return nil, errors.New("disk full")
}
func (failingRepo) Resolve(string, time.Time) ([]*domain.Notification, error) {
	return nil, errors.New("disk full")
}

func TestAFailingStoreNeverFailsThePublisher(t *testing.T) {
	e := notificationstest.NewEnv(t)
	svc, err := notifications.New(failingRepo{e.Repo}, e.Readers(), notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	svc.Subscribe(bus)
	for _, ev := range []domain.Event{
		domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: e.ProjectA, Subject: "x", State: domain.ReviewPending},
		domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: e.ProjectA, Subject: "x", State: domain.ReviewWithdrawn},
		domain.SessionStatusChanged{SessionID: "ghost", Status: domain.SessionWaitingApproval},
	} {
		if err := bus.Publish(ctx, ev); err != nil {
			t.Fatalf("publish %s = %v, want nil", ev.EventName(), err)
		}
	}
}

// faultyRepo fails the next write the way it is told to: its Create, or its
// commit after the write ran.
type faultyRepo struct {
	ports.NotificationRepository
	mu                     sync.Mutex
	failCreate, failCommit bool
}

func (r *faultyRepo) fail(create, commit bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCreate, r.failCommit = create, commit
}

func (r *faultyRepo) Transaction(fn func(tx ports.NotificationRepository) error) error {
	r.mu.Lock()
	create, commit := r.failCreate, r.failCommit
	r.failCreate, r.failCommit = false, false
	r.mu.Unlock()
	return r.NotificationRepository.Transaction(func(tx ports.NotificationRepository) error {
		if create {
			tx = failingRepo{tx}
		}
		if err := fn(tx); err != nil {
			return err
		}
		if commit {
			return errors.New("commit failed")
		}
		return nil
	})
}

// A write that fails, before or at its commit, answers an error and never
// holds back the changes after it: followers still get them.
func TestAFailedWriteNeverHoldsBackTheChangesAfterIt(t *testing.T) {
	e := notificationstest.NewEnv(t)
	repo := &faultyRepo{NotificationRepository: e.Repo}
	svc, err := notifications.New(repo, e.Readers(), notifications.Options{})
	if err != nil {
		t.Fatal(err)
	}
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	changes, _ := svc.FollowNotifications(fctx)

	for _, c := range []struct {
		name           string
		create, commit bool
	}{{"create fails", true, false}, {"commit fails", false, true}} {
		repo.fail(c.create, c.commit)
		if _, err := svc.Notify(ctx, ports.NotifyRequest{ProjectID: e.ProjectA, Title: "lost: " + c.name}); err == nil {
			t.Fatalf("%s: Notify = nil error", c.name)
		}
		want := "after: " + c.name
		if _, err := svc.Notify(ctx, ports.NotifyRequest{ProjectID: e.ProjectA, Title: want}); err != nil {
			t.Fatalf("%s: the next Notify = %v", c.name, err)
		}
		select {
		case ch := <-changes:
			if len(ch.Changed) != 1 || ch.Changed[0].Title != want {
				t.Fatalf("%s: followers got %+v, want only %q", c.name, ch.Changed, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the change after the failed write never reached followers", c.name)
		}
	}
}

func TestNotifyChecksItsInputAndDefaultsTheLink(t *testing.T) {
	r := newRig(t)
	tk := r.task(t, r.ProjectA, "Ship")
	other := r.task(t, r.ProjectB, "Elsewhere")

	cases := []struct {
		name string
		req  ports.NotifyRequest
		code string
	}{
		{"no title", ports.NotifyRequest{ProjectID: r.ProjectA, Title: "   "}, "INVALID_INPUT"},
		{"long title", ports.NotifyRequest{ProjectID: r.ProjectA, Title: strings.Repeat("é", 121)}, "INVALID_INPUT"},
		{"long body", ports.NotifyRequest{ProjectID: r.ProjectA, Title: "t", Body: strings.Repeat("b", 2001)}, "INVALID_INPUT"},
		{"bad level", ports.NotifyRequest{ProjectID: r.ProjectA, Title: "t", Level: "urgent"}, "INVALID_INPUT"},
		{"absolute link", ports.NotifyRequest{ProjectID: r.ProjectA, Title: "t", Link: "https://evil.example/x"}, "INVALID_INPUT"},
		{"protocol-relative link", ports.NotifyRequest{ProjectID: r.ProjectA, Title: "t", Link: "//evil.example/x"}, "INVALID_INPUT"},
		{"no project", ports.NotifyRequest{Title: "t"}, "INVALID_INPUT"},
		{"unknown project", ports.NotifyRequest{ProjectID: "ghost", Title: "t"}, "PROJECT_NOT_FOUND"},
		{"unknown task", ports.NotifyRequest{ProjectID: r.ProjectA, TaskID: "ghost", Title: "t"}, "TICKET_NOT_FOUND"},
		{"task of another project", ports.NotifyRequest{ProjectID: r.ProjectA, TaskID: other.ID, Title: "t"}, "TASK_NOT_ON_PROJECT"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.svc.Notify(ctx, c.req)
			wantCode(t, err, c.code)
		})
	}

	n, err := r.svc.Notify(ctx, ports.NotifyRequest{SessionID: "s1", ProjectID: r.ProjectA, TaskID: tk.ID, Title: "  done  ", Body: " see it ", Level: domain.NotificationAction})
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "done" || n.Body != "see it" || n.Level != domain.NotificationAction || n.Source != domain.NotificationFromAgent ||
		n.Kind != domain.NotificationAgent || n.Link != domain.TaskLink(r.ProjectA, tk.ID) || n.SessionID != "s1" ||
		n.ProjectName != "Alpha" || n.TaskTitle != "Ship" {
		t.Fatalf("notified = %+v", n)
	}
	n, err = r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, Title: "board", Link: "/projects/x?tab=1"})
	if err != nil || n.Level != domain.NotificationInfo || n.Link != "/projects/x?tab=1" {
		t.Fatalf("notified with a link = %+v, %v", n, err)
	}
	n, err = r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, Title: "board"})
	if err != nil || n.Link != domain.ProjectLink(r.ProjectA) {
		t.Fatalf("notified with no task = %+v, %v", n, err)
	}
}

func TestNotifyIsRateLimitedPerSessionAndPerProjectOverARollingHour(t *testing.T) {
	r := newRig(t)
	post := func(session, project string) error {
		_, err := r.svc.Notify(ctx, ports.NotifyRequest{SessionID: session, ProjectID: project, Title: "t"})
		return err
	}
	for i := 0; i < 20; i++ {
		if err := post("s1", r.ProjectA); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		if err := post("", r.ProjectA); err != nil {
			t.Fatalf("global post %d: %v", i, err)
		}
	}
	wantCode(t, post("s1", r.ProjectA), "NOTIFICATION_RATE_LIMITED")
	wantCode(t, post("", r.ProjectA), "NOTIFICATION_RATE_LIMITED")
	if err := post("s2", r.ProjectA); err != nil {
		t.Fatalf("another session: %v", err)
	}
	if err := post("", r.ProjectB); err != nil {
		t.Fatalf("another project: %v", err)
	}
	r.Clock.Advance(time.Hour)
	if err := post("s1", r.ProjectA); err != nil {
		t.Fatalf("an hour later: %v", err)
	}
}

func TestPruneDropsOldReadOrResolvedNotificationsAndKeepsUnreadOnes(t *testing.T) {
	r := newRig(t)
	read, _ := r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, Title: "read"})
	unread, _ := r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, Title: "unread"})
	r.publish(t, domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: r.ProjectA, Subject: "x", State: domain.ReviewPending})
	r.publish(t, domain.ReviewRequestChanged{ReviewID: "rv1", ProjectID: r.ProjectA, Subject: "x", State: domain.ReviewApproved})
	if _, err := r.svc.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{read.ID}}); err != nil {
		t.Fatal(err)
	}
	r.Clock.Advance(notifications.Retention + time.Hour)
	fresh, _ := r.svc.Notify(ctx, ports.NotifyRequest{ProjectID: r.ProjectA, Title: "fresh"})
	if _, err := r.svc.MarkNotificationsRead(ctx, ports.MarkNotificationsRead{IDs: []string{fresh.ID}}); err != nil {
		t.Fatal(err)
	}
	n, err := r.svc.Prune(ctx)
	if err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v; want the read and the resolved one", n, err)
	}
	page := r.list(t, ports.NotificationQuery{})
	if len(page.Notifications) != 2 || page.Notifications[0].ID != fresh.ID || page.Notifications[1].ID != unread.ID {
		t.Fatalf("after prune = %+v", page.Notifications)
	}
}

func TestConcurrentWritersStreamInCommitOrder(t *testing.T) {
	r := newRig(t)
	fctx, cancel := context.WithCancel(ctx)
	defer cancel()
	changes, _ := r.svc.FollowNotifications(fctx)

	const writers, each = 6, 8
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if i%2 == 0 {
					r.publish(t, domain.ReviewRequestChanged{ReviewID: fmt.Sprintf("rv-%d-%d", w, i), ProjectID: r.ProjectA, Subject: "x", State: domain.ReviewPending})
					continue
				}
				if _, err := r.svc.Notify(ctx, ports.NotifyRequest{SessionID: fmt.Sprintf("s-%d", w), ProjectID: r.ProjectB, Title: "t"}); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	for want := 1; want <= writers*each; want++ {
		select {
		case c := <-changes:
			if c.UnreadCount != want || len(c.Changed) != 1 {
				t.Fatalf("change %d carries unread %d and %d notifications; want %d and 1", want, c.UnreadCount, len(c.Changed), want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no change %d", want)
		}
	}
	wg.Wait()
}

func TestAFollowerThatFallsBehindIsClosed(t *testing.T) {
	r := newRig(t)
	changes, _ := r.svc.FollowNotifications(ctx)
	for i := 0; i < 70; i++ {
		if _, err := r.svc.Notify(ctx, ports.NotifyRequest{SessionID: fmt.Sprintf("s%d", i), ProjectID: r.ProjectA, Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for range changes {
		n++
	}
	if n != 64 {
		t.Fatalf("a follower that never read got %d changes before closing, want its buffer of 64", n)
	}
}

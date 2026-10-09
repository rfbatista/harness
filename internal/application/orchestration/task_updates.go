package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Task updates: when the person adds a resource or a comment to a task,
// every running session on it gets a turn saying so (see "Contract: Harness
// server ↔ Agent sessions — task resources, comments and update turns").
// Planning raises the events; the turns go through the courier, so a busy
// session gets its update when its turn ends, never in the middle of one.

// maxUpdateComment is how much of a comment an update turn carries.
const maxUpdateComment = 2000

// taskUpdate is one turn for every running session of a task.
type taskUpdate struct{ ticketID, key, text string }

// updateQueue hands updates to a single drainer, so the bus handler never
// waits on a session and the turns go out in the order the person made the
// changes. The drainer runs only while there is something to deliver.
type updateQueue struct {
	mu       sync.Mutex
	pending  []taskUpdate
	draining bool
}

// SubscribeTaskUpdates turns a person's additions to a task into turns for
// the task's running sessions.
func (s *Service) SubscribeTaskUpdates(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.TicketResourceAdded) error {
		s.queueUpdate(taskUpdate{ev.Resource.TicketID, "task-update:" + ev.Resource.ID, resourceUpdateText(ev.Resource)})
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.TicketCommentAdded) error {
		s.queueUpdate(taskUpdate{ev.Comment.TicketID, "task-update:" + ev.Comment.ID, commentUpdateText(ev.Comment)})
		return nil
	})
}

func (s *Service) queueUpdate(u taskUpdate) {
	q := &s.updates
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, u)
	if !q.draining {
		q.draining = true
		go s.drainUpdates()
	}
}

func (s *Service) drainUpdates() {
	q := &s.updates
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.draining = false
			q.mu.Unlock()
			return
		}
		u := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()
		s.deliverTaskUpdate(u)
	}
}

// deliverTaskUpdate puts u into every session of its task that is still
// running. One that stopped in the meantime is skipped: nothing is queued
// for a session that is not running.
func (s *Service) deliverTaskUpdate(u taskUpdate) {
	deliver := s.deliverUpdate
	if deliver == nil {
		deliver = func(id, key, text string) error {
			_, err := s.Deliver(id, key, text, nil)
			return err
		}
	}
	for _, sess := range s.sessions.List(ports.SessionFilter{TicketID: u.ticketID}) {
		if sess.TicketID != u.ticketID || sess.Status.IsTerminal() {
			continue
		}
		if err := deliver(sess.ID, u.key, u.text); err != nil && errs.Code(err) != "SESSION_NOT_RUNNING" {
			slog.Warn("deliver a task update", "session", sess.ID, "key", u.key, "err", err)
		}
	}
}

// resourceNoun is how an update turn names a resource of kind k.
func resourceNoun(k domain.ResourceKind) string {
	switch k {
	case domain.ResourceImage:
		return "an image"
	case domain.ResourceAudio:
		return "an audio file"
	case domain.ResourceLink:
		return "a link"
	}
	return "a file"
}

func resourceUpdateText(r domain.TicketResource) string {
	ref := "resource_id " + r.ID
	if r.Kind == domain.ResourceLink {
		ref += ", url " + r.URL
	}
	return fmt.Sprintf("[task update · resource added] The person added %s to this task: %q (%s). Read it with read_task_resource if it bears on your work.",
		resourceNoun(r.Kind), r.Title, ref)
}

func commentUpdateText(c domain.TicketComment) string {
	body := c.Body
	if utf8.RuneCountInString(body) > maxUpdateComment {
		body = string([]rune(body)[:maxUpdateComment]) + "…\n(cut; read the whole comment with list_task_comments)"
	}
	return "[task update · comment] The person commented on this task (comment_id " + c.ID + "):\n" + body
}

// taskCounts is how many resources and comments a task has; known is false
// when they could not be read.
type taskCounts struct {
	resources, comments int
	known               bool
}

// resourcesSection is the brief's part on what the person adds to the task.
// Without known counts the rest still holds.
func resourcesSection(c taskCounts) string {
	var b strings.Builder
	b.WriteString("## Resources and comments from the person\n\n")
	b.WriteString("The person can add files, images, audio, links and comments to this task.\n")
	if c.known {
		b.WriteString(countLine(c.resources, c.comments) + "\n")
	}
	b.WriteString(`When they add one while you run, a turn starting with [task update · …]
tells you; read it if it bears on your work.

- mcp__task__list_task_resources — what the person added, oldest first
- mcp__task__read_task_resource — one resource: text inline, or a local path to open
- mcp__task__list_task_comments — the person's comments, oldest first (since narrows)`)
	return b.String()
}

func countLine(resources, comments int) string {
	if resources == 0 && comments == 0 {
		return "This task has no resources and no comments yet."
	}
	return "This task has " + plural(resources, "resource") + " and " + plural(comments, "comment") + "."
}

func plural(n int, noun string) string {
	switch n {
	case 0:
		return "no " + noun + "s"
	case 1:
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// taskCounts counts the task's resources and comments for its brief. A
// failed or missing lookup leaves them unknown: the brief is help, not a
// gate.
func (s *Service) taskCounts(tk *domain.Ticket) taskCounts {
	if s.Resources == nil || s.Comments == nil || tk == nil {
		return taskCounts{}
	}
	ctx := context.Background()
	rs, err := s.Resources.ListTicketResources(ctx, tk.ID)
	if err != nil {
		slog.Warn("count the task's resources for its brief", "ticket", tk.ID, "err", err)
		return taskCounts{}
	}
	cs, err := s.Comments.ListTicketComments(ctx, tk.ID, time.Time{})
	if err != nil {
		slog.Warn("count the task's comments for its brief", "ticket", tk.ID, "err", err)
		return taskCounts{}
	}
	return taskCounts{resources: len(rs), comments: len(cs), known: true}
}

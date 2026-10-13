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

	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Task updates: when the person adds a resource or a comment to a task,
// every running session on it gets a turn saying so, naming the file's path
// in the session's virtual shell (see "Contract: Harness server ↔ Agent
// sessions — virtual shell" §8).
// Planning raises the events; the turns go through the courier, so a busy
// session gets its update when its turn ends, never in the middle of one.
// A comment a session wrote wakes no one: sessions answering each other's
// comments would loop, and they coordinate through task messages instead.

// maxUpdateComment is how much of a comment an update turn carries.
const maxUpdateComment = 2000

// taskUpdate is one turn for every running session of a task. The turn
// names the shell file of the item (kind, id): text makes it from the path
// the session's shell holds it at, or fallback when the shell does not.
type taskUpdate struct {
	ticketID, key      string
	kind, id, fallback string
	text               func(path string) string
}

// shellPathWait is how long an update waits for a session's shell to hold
// the new file, which the shells are written after the change is stored.
const shellPathWait = 3 * time.Second

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
		r := ev.Resource
		u := taskUpdate{ticketID: r.TicketID, key: "task-update:" + r.ID, fallback: linksPath,
			text: func(path string) string { return resourceUpdateText(r, path) }}
		if r.IsUpload() {
			u.kind, u.id, u.fallback = ports.ShellResource, r.ID, "task/resources/"+vshell.ResourceFileName(&r)
		}
		s.queueUpdate(u)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.TicketCommentAdded) error {
		if ev.Comment.Author == domain.CommentBySession {
			return nil
		}
		c := ev.Comment
		s.queueUpdate(taskUpdate{ticketID: c.TicketID, key: "task-update:" + c.ID, kind: ports.ShellComment, id: c.ID,
			fallback: "task/comments/" + vshell.CommentFileName(&c), text: func(path string) string { return commentUpdateText(c, path) }})
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
	// One wait for the shells per update, shared by its sessions, so the
	// courier is held at most shellPathWait whatever their number.
	deadline := time.Now().Add(shellPathWait)
	for _, sess := range s.sessions.List(ports.SessionFilter{TicketID: u.ticketID}) {
		if sess.TicketID != u.ticketID || sess.Status.IsTerminal() {
			continue
		}
		if err := deliver(sess.ID, u.key, u.text(s.shellPath(sess.ID, u, deadline))); err != nil && errs.Code(err) != "SESSION_NOT_RUNNING" {
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

// linksPath is the shell file listing the links the person added.
const linksPath = "task/resources/links.md"

// shellPath is where the session's shell holds u's file: once the shell
// has it, the path it has it at, so a name that clashed is named as the
// shell has it. A session with no shell, or a shell that has not got the
// file by deadline, gets the name the shell gives such a file.
func (s *Service) shellPath(sessionID string, u taskUpdate, deadline time.Time) string {
	if s.ShellIndex == nil || u.kind == "" {
		return u.fallback
	}
	for {
		files := s.ShellIndex.ShellFiles(sessionID)
		if files == nil {
			return u.fallback
		}
		for _, f := range files {
			if f.Kind == u.kind && f.ID == u.id {
				return f.Path
			}
		}
		if time.Now().After(deadline) {
			return u.fallback
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func resourceUpdateText(r domain.TicketResource, path string) string {
	if r.Kind == domain.ResourceLink {
		return fmt.Sprintf("[task update · resource added] The person added a link to this task: %q (%s), listed in %s in your shell. Open it if it bears on your work.",
			r.Title, r.URL, path)
	}
	return fmt.Sprintf("[task update · resource added] The person added %s to this task: %q, at %s in your shell. Read it there if it bears on your work.",
		resourceNoun(r.Kind), r.Title, path)
}

func commentUpdateText(c domain.TicketComment, path string) string {
	body := c.Body
	if utf8.RuneCountInString(body) > maxUpdateComment {
		body = string([]rune(body)[:maxUpdateComment]) + "…\n(cut; the whole comment is " + path + " in your shell)"
	}
	return "[task update · comment] The person commented on this task (" + path + " in your shell):\n" + body
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
	b.WriteString("## Resources and comments\n\n")
	b.WriteString("The person can add files, images, audio, links and comments to this task.\n")
	if c.known {
		b.WriteString(countLine(c.resources, c.comments) + "\n")
	}
	b.WriteString(`They are files in your virtual shell: uploads in task/resources/, links in
task/resources/links.md, comments in task/comments/, one file each, the
person's and sessions'. When they add a resource or comment while you run, a
turn starting with [task update · …] names its file; read it if it bears on
your work.

You can comment on this task too, in the thread the person reads: write a
file in task/comments/ (any name, e.g. progress.md); its content is the
comment. A comment is for what the person should see on the task: progress,
a decision, a question for them, a summary of what you did. It is not for
talking to other sessions: your comment wakes no session, so reach them with
the task messages (message_architect, reply_to_session) when the task has
them. Edit or rm only your own comments; the others' are read-only.`)
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

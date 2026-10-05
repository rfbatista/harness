// Package live keeps tui-client's lists current: it follows the session feed
// of the project the user is in, and turns each change into a message the
// screens apply. When the stream is lost it says so, retries with backoff, and
// on reconnecting tells the screens to reload: changes were missed meanwhile.
package live

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Messages for the screens.
type (
	// ChangeMsg is one change to a session of the followed project.
	ChangeMsg struct {
		ProjectID string
		Change    ports.SessionChange
	}
	// ResyncMsg says changes were missed: reload the project's sessions.
	ResyncMsg struct{ ProjectID string }
)

// The follower's own messages; a gen that is not current is a follow since
// replaced, and is ignored.
type (
	connectedMsg struct {
		gen     int
		changes <-chan ports.SessionChange
		err     error
	}
	nextMsg struct {
		gen    int
		change ports.SessionChange
		ok     bool
	}
	retryMsg struct{ gen int }
)

const (
	firstRetry = time.Second
	maxRetry   = 30 * time.Second
)

// Follower follows one project at a time.
type Follower struct {
	feed ports.SessionFeed

	projectID string
	gen       int
	cancel    context.CancelFunc
	changes   <-chan ports.SessionChange
	lost      bool // the stream was lost and is not back yet
	missed    bool // changes may have been missed since the last (re)connect
	retry     time.Duration
}

// New returns a follower of feed, following nothing yet.
func New(feed ports.SessionFeed) *Follower { return &Follower{feed: feed, retry: firstRetry} }

// Project is the project being followed, or "".
func (f *Follower) Project() string { return f.projectID }

// Lost reports whether the stream is down and being retried.
func (f *Follower) Lost() bool { return f.lost }

// Follow switches to projectID; "" stops following.
func (f *Follower) Follow(projectID string) tea.Cmd {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.gen++
	f.projectID, f.changes, f.lost, f.missed, f.retry = projectID, nil, false, false, firstRetry
	if projectID == "" {
		return nil
	}
	return f.connect()
}

func (f *Follower) connect() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	feed, pid, gen := f.feed, f.projectID, f.gen
	return func() tea.Msg {
		changes, err := feed.FollowProject(ctx, pid)
		return connectedMsg{gen: gen, changes: changes, err: err}
	}
}

func (f *Follower) wait() tea.Cmd {
	changes, gen := f.changes, f.gen
	return func() tea.Msg {
		c, ok := <-changes
		return nextMsg{gen: gen, change: c, ok: ok}
	}
}

// Update handles the follower's own messages and reports whether msg was one.
// The commands it returns deliver ChangeMsg and ResyncMsg to the screens.
func (f *Follower) Update(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case connectedMsg:
		if msg.gen != f.gen {
			return nil, true
		}
		if msg.err != nil {
			return f.lose(), true
		}
		f.changes, f.lost, f.retry = msg.changes, false, firstRetry
		cmds := []tea.Cmd{f.wait()}
		if f.missed {
			f.missed = false
			pid := f.projectID
			cmds = append(cmds, func() tea.Msg { return ResyncMsg{ProjectID: pid} })
		}
		return tea.Batch(cmds...), true
	case nextMsg:
		if msg.gen != f.gen {
			return nil, true
		}
		if !msg.ok {
			return f.lose(), true
		}
		pid, change := f.projectID, msg.change
		return tea.Batch(f.wait(), func() tea.Msg { return ChangeMsg{ProjectID: pid, Change: change} }), true
	case retryMsg:
		if msg.gen != f.gen {
			return nil, true
		}
		return f.connect(), true
	}
	return nil, false
}

// lose marks the stream down and schedules a retry, backing off.
func (f *Follower) lose() tea.Cmd {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	f.lost, f.missed, f.changes = true, true, nil
	wait, gen := f.retry, f.gen
	f.retry = min(2*f.retry, maxRetry)
	return tea.Tick(wait, func(time.Time) tea.Msg { return retryMsg{gen: gen} })
}

// Apply is list with change applied: the session replaced, added, or removed.
// keep says whether a session belongs in the list at all (a task screen
// keeps its own task's).
func Apply(list []*domain.Session, change ports.SessionChange, keep func(*domain.Session) bool) []*domain.Session {
	s := change.Session
	out := make([]*domain.Session, 0, len(list)+1)
	found := false
	for _, x := range list {
		if x.ID != s.ID {
			out = append(out, x)
			continue
		}
		found = true
		if !change.Deleted {
			out = append(out, s)
		}
	}
	if !found && !change.Deleted && keep(s) {
		out = append(out, s)
	}
	return out
}

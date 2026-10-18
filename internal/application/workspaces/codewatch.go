package workspaces

import (
	"context"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// CodeWatchInterval is how often a running session's worktree is looked at,
// and so the least time between two code_changed of one workspace.
const CodeWatchInterval = 2 * time.Second

// CodeWatcherDeps is what the watcher needs.
type CodeWatcherDeps struct {
	Sessions   ports.SessionRepository
	Workspaces ports.WorkspaceRepository
	Git        ports.GitCode
	Feed       ports.ProjectChangeSink
	// Interval is CodeWatchInterval when zero.
	Interval time.Duration
	// Now is the clock the interval is measured with; nil is time.Now.
	Now func() time.Time
}

// CodeWatcher announces code_changed on the project feed when the worktree
// of a workspace whose session runs moves: its HEAD or its working tree. It
// follows sessions through their domain events and polls each watched
// worktree's git status, so it needs no file watches and ignores what git
// ignores. It polls a worktree only while someone follows its project's feed
// (ProjectFollowers): with nobody to tell, it runs no git at all.
type CodeWatcher struct {
	d CodeWatcherDeps

	mu       sync.Mutex
	sessions map[string]string           // running session → its workspace
	watched  map[string]*watchedWorktree // workspace → what was last seen
	followed map[string]bool             // project whose feed has a follower

	stop chan struct{}
	done chan struct{}
}

type watchedWorktree struct {
	projectID   string
	path        string
	sessions    int
	fingerprint string // "" until first seen
	announced   time.Time
}

// NewCodeWatcher returns a watcher; Start begins polling.
func NewCodeWatcher(d CodeWatcherDeps) *CodeWatcher {
	if d.Interval <= 0 {
		d.Interval = CodeWatchInterval
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &CodeWatcher{
		d: d, sessions: map[string]string{}, watched: map[string]*watchedWorktree{}, followed: map[string]bool{},
	}
}

// Subscribe follows sessions starting, changing status, ending and being
// deleted.
func (w *CodeWatcher) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SessionStarted) error {
		w.Track(ev.SessionID)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionStatusChanged) error {
		if ev.Status.IsTerminal() {
			w.Untrack(ev.SessionID)
		} else {
			w.Track(ev.SessionID)
		}
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionEnded) error {
		w.Untrack(ev.SessionID)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionDeleted) error {
		w.Untrack(ev.SessionID)
		return nil
	})
}

// runningStatuses are the statuses of a session whose process is alive.
var runningStatuses = []domain.SessionStatus{
	domain.SessionStarting, domain.SessionRunning, domain.SessionIdle, domain.SessionThinking,
	domain.SessionWaitingApproval, domain.SessionPaused,
}

// Start watches the sessions already running and polls every interval until
// Stop.
func (w *CodeWatcher) Start() {
	for _, s := range w.d.Sessions.List(ports.SessionFilter{Statuses: runningStatuses}) {
		w.Track(s.ID)
	}
	w.stop, w.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(w.done)
		t := time.NewTicker(w.d.Interval)
		defer t.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-t.C:
				w.Poll(context.Background())
			}
		}
	}()
}

// Stop ends polling and waits for the round in progress.
func (w *CodeWatcher) Stop() {
	if w.stop == nil {
		return
	}
	close(w.stop)
	<-w.done
	w.stop = nil
}

// Track watches the session's workspace while the session runs. A session
// without a workspace, or already tracked, changes nothing.
func (w *CodeWatcher) Track(sessionID string) {
	w.mu.Lock()
	_, known := w.sessions[sessionID]
	w.mu.Unlock()
	if known {
		return
	}
	s := w.d.Sessions.Get(sessionID)
	if s == nil || s.WorkspaceID == "" || s.Status.IsTerminal() {
		return
	}
	ws := w.d.Workspaces.Get(s.WorkspaceID)
	if ws == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, known := w.sessions[sessionID]; known {
		return
	}
	w.sessions[sessionID] = ws.ID
	if wt := w.watched[ws.ID]; wt != nil {
		wt.sessions++
		return
	}
	w.watched[ws.ID] = &watchedWorktree{projectID: s.ProjectID, path: ws.Path, sessions: 1}
}

// Untrack stops watching for the session; its workspace stays watched while
// another running session has it.
func (w *CodeWatcher) Untrack(sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	wsID, ok := w.sessions[sessionID]
	if !ok {
		return
	}
	delete(w.sessions, sessionID)
	if wt := w.watched[wsID]; wt != nil {
		if wt.sessions--; wt.sessions <= 0 {
			delete(w.watched, wsID)
		}
	}
}

// ProjectFollowers polls the project's worktrees while its feed has a
// follower and stops when the last one leaves (ports.FeedFollowersObserver).
// What was seen is forgotten on leaving, so the first look after a new
// follower arrives only records, as on a session's start.
func (w *CodeWatcher) ProjectFollowers(projectID string, followers int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if followers > 0 {
		w.followed[projectID] = true
		return
	}
	delete(w.followed, projectID)
	for _, wt := range w.watched {
		if wt.projectID == projectID {
			wt.fingerprint = ""
		}
	}
}

// Watched is the workspaces being watched.
func (w *CodeWatcher) Watched() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	ids := make([]string, 0, len(w.watched))
	for id := range w.watched {
		ids = append(ids, id)
	}
	return ids
}

// Poll looks once at every watched worktree whose project's feed is
// followed and announces each one that changed since it was last seen, at
// most once per interval. The first look only records. A worktree gone from
// disk is skipped.
func (w *CodeWatcher) Poll(ctx context.Context) {
	w.mu.Lock()
	type target struct {
		id, path string
	}
	targets := make([]target, 0, len(w.watched))
	for id, wt := range w.watched {
		if w.followed[wt.projectID] {
			targets = append(targets, target{id, wt.path})
		}
	}
	w.mu.Unlock()

	for _, t := range targets {
		callCtx, cancel := context.WithTimeout(ctx, w.d.Interval)
		head, fingerprint, err := w.d.Git.Snapshot(callCtx, t.path)
		cancel()
		if err != nil {
			continue
		}
		w.mu.Lock()
		wt := w.watched[t.id]
		var change *ports.CodeChange
		switch {
		case wt == nil: // untracked meanwhile
		case !w.followed[wt.projectID]: // the last follower left meanwhile
		case wt.fingerprint == "":
			wt.fingerprint = fingerprint
		case wt.fingerprint != fingerprint && w.d.Now().Sub(wt.announced) >= w.d.Interval:
			wt.fingerprint, wt.announced = fingerprint, w.d.Now()
			change = &ports.CodeChange{WorkspaceID: t.id, Head: head, ProjectID: wt.projectID}
		}
		w.mu.Unlock()
		if change != nil && w.d.Feed != nil {
			w.d.Feed.AnnounceChange(ports.ProjectChange{CodeChanged: change})
		}
	}
}

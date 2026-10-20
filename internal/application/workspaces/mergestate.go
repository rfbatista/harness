package workspaces

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TicketDecorator = (*MergeStates)(nil)

// MergeStateDeps is what the merge state of a task is read from: its
// sessions, their workspaces and repositories, and git.
type MergeStateDeps struct {
	Sessions     ports.SessionRepository
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	Git          ports.BranchMergeReader
	// Tickets tells which tasks a change concerns are in review or done;
	// Announcer puts a task whose merge state changed on the feed again. A nil
	// Announcer refreshes in the background without telling anyone.
	Tickets   ports.TicketRepository
	Announcer ports.TicketReannouncer
	// Now is the clock MergeStateFreshFor is measured with; nil is time.Now.
	Now func() time.Time
}

// MergeStateFreshFor is how long a computed merge state is served before a
// read refreshes it in the background.
const MergeStateFreshFor = 30 * time.Second

// MergeStates says how much of each review or done task's code has landed in
// its repository's default branch. Reading git costs a few runs per branch,
// so the state is kept per task and served at once; it is computed again in
// the background when it is older than MergeStateFreshFor or computed for
// other branches, when a workspace of the task's sessions changes code or
// its pull request changes (AnnounceChange), and the task is announced again
// when its state changed.
type MergeStates struct {
	d MergeStateDeps

	// The background refreshes run under ctx, which Close ends.
	ctx    context.Context
	cancel context.CancelFunc
	bg     sync.WaitGroup

	mu      sync.Mutex
	cache   map[string]mergeEntry // by task
	queue   []string              // tasks to refresh in the background
	queued  map[string]bool
	working bool
	closed  bool
}

// mergeEntry is a task's merge state, the branches it was computed from (a
// task given another session's branch since is computed again) and when.
type mergeEntry struct {
	branches string
	state    domain.MergeState
	at       time.Time
}

// taskBranches are a task's branches, by repository.
type taskBranches map[string][]ports.BranchBase

// key names the branches, whatever their order.
func (b taskBranches) key() string {
	var parts []string
	for repo, branches := range b {
		for _, br := range branches {
			parts = append(parts, repo+"\x00"+br.Branch+"\x00"+br.Base)
		}
	}
	slices.Sort(parts)
	return strings.Join(parts, "\x01")
}

// NewMergeStates returns the use case.
func NewMergeStates(d MergeStateDeps) *MergeStates {
	if d.Now == nil {
		d.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &MergeStates{d: d, ctx: ctx, cancel: cancel, cache: map[string]mergeEntry{}, queued: map[string]bool{}}
}

// DecorateTickets fills the merge state of each review or done task that is
// not archived, and clears every other's. It reads the tasks' branches from
// the database at once and serves what is kept; git runs in the background
// for a stale or missing state, which a list leaves null meanwhile. A single
// task is computed at once when it has none for its branches.
func (s *MergeStates) DecorateTickets(ctx context.Context, tickets ...*domain.Ticket) {
	var due []*domain.Ticket
	for _, tk := range tickets {
		if tk == nil {
			continue
		}
		tk.MergeState = nil
		if tk.ArchivedAt == nil && (tk.Status == domain.TicketStatusReview || tk.Status == domain.TicketStatusDone) {
			due = append(due, tk)
		}
	}
	if len(due) == 0 {
		return
	}
	ids := make([]string, len(due))
	for i, tk := range due {
		ids[i] = tk.ID
	}
	byTask := s.branches(ctx, ids...)
	now := s.d.Now()
	for _, tk := range due {
		branches := byTask[tk.ID]
		s.mu.Lock()
		e, ok := s.cache[tk.ID]
		s.mu.Unlock()
		same := ok && e.branches == branches.key()
		switch {
		case same && now.Sub(e.at) < MergeStateFreshFor:
		case len(due) == 1 && !same:
			var err error
			if e.state, err = s.store(ctx, tk.ID, branches); err != nil {
				slog.Warn("read task merge state", "ticket", tk.ID, "err", err)
			}
			ok = true
		default:
			s.enqueue(tk.ID)
		}
		if ok {
			st := e.state
			tk.MergeState = &st
		}
	}
}

// Refresh computes the task's merge state from git and keeps it.
func (s *MergeStates) Refresh(ctx context.Context, taskID string) (domain.MergeState, error) {
	return s.store(ctx, taskID, s.branches(ctx, taskID)[taskID])
}

// store computes the merge state of the task's branches and keeps it. A
// failed read is unknown, and kept nothing.
func (s *MergeStates) store(ctx context.Context, taskID string, branches taskBranches) (domain.MergeState, error) {
	st, err := s.compute(ctx, branches)
	if err != nil {
		return domain.MergeState{State: domain.MergeUnknown}, err
	}
	s.mu.Lock()
	s.cache[taskID] = mergeEntry{branches: branches.key(), state: st, at: s.d.Now()}
	s.mu.Unlock()
	return st, nil
}

// branches reads the branches of the given tasks' sessions' workspaces, in
// two queries.
func (s *MergeStates) branches(ctx context.Context, taskIDs ...string) map[string]taskBranches {
	var wsIDs []string
	taskOf := map[string][]string{} // tasks by workspace
	for _, sess := range s.d.Sessions.List(ctx, ports.SessionFilter{TicketIDs: taskIDs}) {
		if sess.WorkspaceID == "" {
			continue
		}
		if _, ok := taskOf[sess.WorkspaceID]; !ok {
			wsIDs = append(wsIDs, sess.WorkspaceID)
		}
		taskOf[sess.WorkspaceID] = append(taskOf[sess.WorkspaceID], sess.TicketID)
	}
	out := map[string]taskBranches{}
	for _, ws := range s.d.Workspaces.ListByIDs(ctx, wsIDs) {
		if ws.Branch == "" {
			continue
		}
		for _, task := range taskOf[ws.ID] {
			if out[task] == nil {
				out[task] = taskBranches{}
			}
			b := ports.BranchBase{Branch: ws.Branch, Base: ws.BaseRef, BaseSHA: ws.BaseSHA}
			if !slices.Contains(out[task][ws.RepositoryID], b) {
				out[task][ws.RepositoryID] = append(out[task][ws.RepositoryID], b)
			}
		}
	}
	return out
}

// compute counts the branches with commits of their own, and how many of
// them their repository's default branch contains. A branch that is gone,
// or a repository without a default branch, cannot be measured: the state is
// unknown.
func (s *MergeStates) compute(ctx context.Context, byRepo taskBranches) (domain.MergeState, error) {
	unknown := domain.MergeState{State: domain.MergeUnknown}
	var merged, total int
	for repoID, branches := range byRepo {
		repo := s.d.Repositories.Get(ctx, repoID)
		if repo == nil {
			return unknown, nil
		}
		res, err := s.d.Git.BranchMerges(ctx, repo.RootDir, branches)
		if err != nil {
			return unknown, err
		}
		if res.DefaultBranch == "" {
			return unknown, nil
		}
		for _, b := range branches {
			m := res.Branches[b.Branch]
			if !m.Exists {
				return unknown, nil
			}
			if !m.Ahead {
				continue
			}
			total++
			if m.Merged {
				merged++
			}
		}
	}
	st := domain.MergeState{Merged: merged, Total: total}
	switch {
	case total == 0:
		st.State = domain.MergeNone
	case merged == total:
		st.State = domain.MergeMerged
	case merged == 0:
		st.State = domain.MergeNotMerged
	default:
		st.State = domain.MergePartial
	}
	return st, nil
}

// AnnounceChange hears the project feed's code and pull request changes: the
// tasks whose sessions have the changed workspace are refreshed in the
// background (ports.ProjectChangeSink). It never waits on git.
func (s *MergeStates) AnnounceChange(c ports.ProjectChange) {
	var projectID, workspaceID string
	switch {
	case c.CodeChanged != nil:
		projectID, workspaceID = c.CodeChanged.ProjectID, c.CodeChanged.WorkspaceID
	case c.PullRequest != nil:
		projectID, workspaceID = c.PullRequest.ProjectID, c.PullRequest.WorkspaceID
	default:
		return
	}
	if workspaceID == "" {
		return
	}
	for _, sess := range s.d.Sessions.List(s.ctx, ports.SessionFilter{ProjectID: projectID}) {
		if sess.WorkspaceID == workspaceID && sess.TicketID != "" {
			s.enqueue(sess.TicketID)
		}
	}
}

// enqueue has the task refreshed in the background, once however often it
// is asked before its turn comes. One worker refreshes the queue in order.
func (s *MergeStates) enqueue(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.queued[taskID] {
		return
	}
	s.queued[taskID] = true
	s.queue = append(s.queue, taskID)
	if !s.working {
		s.working = true
		s.bg.Add(1)
		go s.work()
	}
}

func (s *MergeStates) work() {
	defer s.bg.Done()
	for {
		s.mu.Lock()
		if s.closed || len(s.queue) == 0 {
			s.working = false
			s.mu.Unlock()
			return
		}
		taskID := s.queue[0]
		s.queue = s.queue[1:]
		delete(s.queued, taskID)
		s.mu.Unlock()
		s.refreshAndAnnounce(s.ctx, taskID)
	}
}

// refreshAndAnnounce computes the merge state of a review or done task that
// is not archived and announces the task when the state changed (or was never
// computed). Any other task is left alone: it carries no merge state.
func (s *MergeStates) refreshAndAnnounce(ctx context.Context, taskID string) {
	tk := s.d.Tickets.Get(ctx, taskID)
	if tk == nil || tk.ArchivedAt != nil || (tk.Status != domain.TicketStatusReview && tk.Status != domain.TicketStatusDone) {
		return
	}
	s.mu.Lock()
	prev, had := s.cache[taskID]
	s.mu.Unlock()
	st, err := s.Refresh(ctx, taskID)
	if err != nil {
		slog.Warn("refresh task merge state", "ticket", taskID, "err", err)
		return
	}
	if (had && prev.state == st) || s.d.Announcer == nil {
		return
	}
	s.d.Announcer.ReannounceTicket(ctx, taskID)
}

// Wait returns when the background refreshes queued so far are done.
func (s *MergeStates) Wait() { s.bg.Wait() }

// Close ends the background refreshes and waits for the one in progress.
func (s *MergeStates) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.bg.Wait()
}

package workspaces

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

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
}

// MergeStates says how much of each review or done task's code has landed in
// its repository's default branch. Reading git costs a few runs per branch,
// so the state is kept per task: a read serves what was last computed for
// the task's current branches, and computes only when it has none.
type MergeStates struct {
	d MergeStateDeps

	mu    sync.Mutex
	cache map[string]mergeEntry // by task
}

// mergeEntry is a task's merge state and the branches it was computed from:
// a task given another session's branch since is computed again.
type mergeEntry struct {
	branches string
	state    domain.MergeState
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
	return &MergeStates{d: d, cache: map[string]mergeEntry{}}
}

// DecorateTickets fills the merge state of each review or done task that is
// not archived, and clears every other's. It reads the tasks' branches from
// the database at once, and git only for a task not computed yet.
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
	for _, tk := range due {
		branches := byTask[tk.ID]
		st, ok := s.cached(tk.ID, branches.key())
		if !ok {
			var err error
			if st, err = s.store(ctx, tk.ID, branches); err != nil {
				slog.Warn("read task merge state", "ticket", tk.ID, "err", err)
			}
		}
		tk.MergeState = &st
	}
}

func (s *MergeStates) cached(taskID, branches string) (domain.MergeState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[taskID]
	return e.state, ok && e.branches == branches
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
	s.cache[taskID] = mergeEntry{branches: branches.key(), state: st}
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
			b := ports.BranchBase{Branch: ws.Branch, Base: ws.BaseRef}
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

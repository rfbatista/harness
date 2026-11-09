package branchwatch

import (
	"context"
	"slices"
	"sort"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SessionBranches implements ports.SessionBranchLists.
func (s *Service) SessionBranches(ctx context.Context, sessionID string) ([]domain.SessionBranch, error) {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	reqs, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	in, err := s.inputs(ctx, []*domain.Session{sess}, map[string][]domain.BranchRequest{sessionID: reqs})
	if err != nil {
		return nil, err
	}
	return in.build(sess), nil
}

// TaskSessionBranches implements ports.SessionBranchLists.
func (s *Service) TaskSessionBranches(ctx context.Context, taskID string) ([]domain.SessionBranches, error) {
	found, err := s.d.Sessions.Query(ctx, ports.SessionFilter{TicketID: taskID})
	if err != nil {
		return nil, err
	}
	reqs, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{TaskID: taskID})
	if err != nil {
		return nil, err
	}
	bySession := map[string][]domain.BranchRequest{}
	for _, r := range reqs {
		bySession[r.SessionID] = append(bySession[r.SessionID], r)
	}
	in, err := s.inputs(ctx, found, bySession)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SessionBranches, 0, len(found))
	for _, sess := range found {
		out = append(out, domain.SessionBranches{SessionID: sess.ID, Branches: in.build(sess)})
	}
	return out, nil
}

// listInputs is the stored state the lists of some sessions are built from.
type listInputs struct {
	reqs     map[string][]domain.BranchRequest
	stats    map[string]map[string]ports.BranchStat
	findings map[string][]ports.BranchFinding
	pushes   map[string][]ports.RecordedPush // by session id
	baseRef  map[string]string               // by session id
}

func (s *Service) inputs(ctx context.Context, sessions []*domain.Session, reqs map[string][]domain.BranchRequest) (*listInputs, error) {
	in := &listInputs{
		reqs: reqs, stats: map[string]map[string]ports.BranchStat{}, findings: map[string][]ports.BranchFinding{},
		pushes: map[string][]ports.RecordedPush{}, baseRef: map[string]string{},
	}
	ids := make([]string, 0, len(sessions))
	repos := map[string]bool{}
	for _, sess := range sessions {
		ids = append(ids, sess.ID)
		repos[sess.RepositoryID] = true
		if ws := s.workspaceOf(ctx, sess); ws != nil {
			in.baseRef[sess.ID] = ws.BaseRef
		}
	}
	stats, err := s.d.Store.Stats(ctx, ids...)
	if err != nil {
		return nil, err
	}
	for _, st := range stats {
		if in.stats[st.SessionID] == nil {
			in.stats[st.SessionID] = map[string]ports.BranchStat{}
		}
		in.stats[st.SessionID][st.Branch] = st
	}
	if len(ids) > 0 {
		fs, err := s.d.Store.Findings(ctx, "", ids...)
		if err != nil {
			return nil, err
		}
		for _, f := range fs {
			in.findings[f.SessionID] = append(in.findings[f.SessionID], f)
		}
	}
	if s.d.Pushes != nil {
		for repo := range repos {
			ps, err := s.d.Pushes.List(ctx, repo)
			if err != nil {
				return nil, err
			}
			for _, p := range ps {
				in.pushes[p.SessionID] = append(in.pushes[p.SessionID], p)
			}
		}
	}
	return in, nil
}

func (s *Service) workspaceOf(ctx context.Context, sess *domain.Session) *domain.Workspace {
	if sess.WorkspaceID == "" {
		return nil
	}
	return s.d.Workspaces.Get(ctx, sess.WorkspaceID)
}

// row is a list row while it is being built.
type row struct {
	b        domain.SessionBranch
	activity time.Time
}

// answered says the person approved the request, used or not.
func answered(r domain.BranchRequest) bool {
	switch r.State {
	case domain.BranchRequestApproved, domain.BranchRequestUsed:
		return true
	case domain.BranchRequestExpired:
		return r.AnsweredAt != nil
	}
	return false
}

func (r row) touch(t time.Time) row {
	if t.After(r.activity) {
		r.activity = t
	}
	return r
}

// build is the session's list: its branch, then every branch a request or a
// finding names, by last activity.
func (in *listInputs) build(sess *domain.Session) []domain.SessionBranch {
	rows := map[string]*row{}
	get := func(name string) *row {
		if rows[name] == nil {
			rows[name] = &row{b: domain.SessionBranch{
				Name: name, IsSessionBranch: name == sess.Branch, State: domain.SessionBranchApproved,
				Pushed: []domain.SessionBranchPush{}, RequestIDs: []string{},
			}}
		}
		return rows[name]
	}
	get(sess.Branch).b.State = domain.SessionBranchSession
	touched := map[string]bool{}
	settle := func(name string, state domain.SessionBranchState, r domain.BranchRequest) {
		x := get(name)
		touched[name] = true
		x.b.RequestIDs = append(x.b.RequestIDs, r.ID)
		at := r.RequestedAt
		if r.AnsweredAt != nil {
			at = *r.AnsweredAt
		}
		if at.After(x.activity) {
			x.activity = at
		}
		if state != "" && !x.b.IsSessionBranch {
			x.b.State = state
		}
	}
	reqs := in.reqs[sess.ID]                  // newest first
	for _, r := range slices.Backward(reqs) { // oldest first: later requests settle the state
		if r.State == domain.BranchRequestPending {
			continue
		}
		state := domain.SessionBranchApproved
		if r.State == domain.BranchRequestDenied {
			state = domain.SessionBranchDenied
		} else if !answered(r) {
			continue
		}
		switch r.Action {
		case domain.BranchCreate, domain.BranchSwitch, domain.BranchPush:
			if r.Branch != "" {
				settle(r.Branch, state, r)
			}
		case domain.BranchDelete:
			if state == domain.SessionBranchApproved && r.State == domain.BranchRequestUsed {
				state = domain.SessionBranchDeleted
			}
			settle(r.Branch, state, r)
		case domain.BranchRename:
			old := state
			if state == domain.SessionBranchApproved && r.State == domain.BranchRequestUsed {
				old = domain.SessionBranchDeleted
			}
			settle(r.Branch, old, r)
			if r.NewName != nil {
				settle(*r.NewName, state, r)
			}
		}
	}
	// Request ids newest first, as the requests came.
	for name := range touched {
		slices.Reverse(rows[name].b.RequestIDs)
	}
	for _, f := range in.findings[sess.ID] {
		if f.Kind == ports.FindingBranch {
			x := get(f.Branch)
			if !x.b.IsSessionBranch && x.b.State != domain.SessionBranchApproved {
				x.b.State = domain.SessionBranchUnapproved
			}
			if !x.b.IsSessionBranch && len(x.b.RequestIDs) == 0 {
				x.b.State = domain.SessionBranchUnapproved
			}
		}
		if f.Kind == ports.FindingPush {
			x := get(f.Branch)
			if len(x.b.RequestIDs) == 0 && !x.b.IsSessionBranch {
				x.b.State = domain.SessionBranchUnapproved
			}
			x.b.Pushed = append(x.b.Pushed, domain.SessionBranchPush{Remote: f.Remote, RemoteBranch: f.RemoteBranch, At: f.FoundAt, By: domain.PushedByUnapproved})
		}
		x := get(f.Branch)
		t := f.FoundAt
		x.b.FlaggedAt = &t
		if f.FoundAt.After(x.activity) {
			x.activity = f.FoundAt
		}
	}
	for _, p := range in.pushes[sess.ID] {
		x := get(p.Branch)
		x.b.Pushed = append(x.b.Pushed, domain.SessionBranchPush{Remote: p.Remote, RemoteBranch: p.RemoteBranch, At: p.At, By: p.By})
		if p.At.After(x.activity) {
			x.activity = p.At
		}
	}
	for name, x := range rows {
		sort.SliceStable(x.b.Pushed, func(i, j int) bool { return x.b.Pushed[i].At.After(x.b.Pushed[j].At) })
		if x.b.State == domain.SessionBranchDeleted {
			continue
		}
		if st, ok := in.stats[sess.ID][name]; ok && st.BaseRef != "" {
			x.b.Base = &domain.BranchBase{Ref: st.BaseRef, Ahead: st.Ahead, Behind: st.Behind}
		}
	}
	out := make([]*row, 0, len(rows))
	for _, x := range rows {
		out = append(out, x)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.b.IsSessionBranch != b.b.IsSessionBranch {
			return a.b.IsSessionBranch
		}
		if !a.activity.Equal(b.activity) {
			return a.activity.After(b.activity)
		}
		return a.b.Name < b.b.Name
	})
	list := make([]domain.SessionBranch, len(out))
	for i, x := range out {
		list[i] = x.b
	}
	return list
}

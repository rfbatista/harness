package branchwatch

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// turnEnded gathers the turn ends of a repository into one check, run after
// the turn is recorded and never twice at once for one repository.
func (s *Service) turnEnded(sessionID string) {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil || sess.RepositoryID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rc := s.repos[sess.RepositoryID]
	if rc == nil {
		rc = &repoCheck{ended: map[string]bool{}}
		s.repos[sess.RepositoryID] = rc
	}
	rc.ended[sessionID] = true
	s.arm(sess.RepositoryID, rc)
}

// arm starts the debounce timer unless a check is running (its end re-arms).
func (s *Service) arm(repoID string, rc *repoCheck) {
	if rc.running || rc.timer != nil {
		return
	}
	rc.timer = time.AfterFunc(s.d.Debounce, func() { s.run(repoID, rc) })
}

func (s *Service) run(repoID string, rc *repoCheck) {
	s.mu.Lock()
	rc.timer, rc.running = nil, true
	ended := make([]string, 0, len(rc.ended))
	for id := range rc.ended {
		ended = append(ended, id)
	}
	rc.ended = map[string]bool{}
	s.mu.Unlock()
	s.check(context.Background(), repoID, ended)
	s.mu.Lock()
	rc.running = false
	if len(rc.ended) > 0 {
		s.arm(repoID, rc)
	}
	s.mu.Unlock()
}

// Check runs one check of the repository now, as if the sessions' turns had
// just ended; the turn ends do it on their own.
func (s *Service) Check(ctx context.Context, repositoryID string, ended ...string) {
	s.check(ctx, repositoryID, ended)
}

type finding struct {
	kind                                          domain.BranchFindingKind
	branch, remote, remoteBranch, commit, newName string
	// owners are the sessions the finding may belong to; nil is every session.
	owners []*domain.Session
}

func (s *Service) check(ctx context.Context, repoID string, ended []string) {
	repo := s.d.Repositories.Get(ctx, repoID)
	if repo == nil || repo.RootDir == "" {
		return
	}
	snap, err := s.d.Evidence.BranchSnapshot(ctx, repo.RootDir)
	if err != nil {
		slog.Warn("check a repository's branches", "repository", repoID, "err", err)
		return
	}
	now := s.d.Now()
	cur := snapshotOf(snap, now)
	sessions := s.sessionsOf(ctx, repo)
	prev, err := s.d.Store.Baseline(ctx, repoKey(repoID))
	if err != nil {
		slog.Warn("read a repository's latest snapshot", "repository", repoID, "err", err)
		return
	}
	// A session of before this shipped has no starting snapshot: it gets this
	// one, so nothing earlier is attributed to it.
	baselines := map[string]*ports.BranchBaseline{}
	for _, sess := range sessions {
		b, err := s.d.Store.Baseline(ctx, sessionKey(sess.ID))
		if err != nil {
			// A read that failed is not a missing snapshot: leave the session's alone.
			slog.Warn("read a session's starting snapshot", "session", sess.ID, "err", err)
			continue
		}
		if b == nil {
			b = &cur
			if err := s.d.Store.SaveBaseline(ctx, sessionKey(sess.ID), cur); err != nil {
				slog.Warn("store a session's starting snapshot", "session", sess.ID, "err", err)
			}
		}
		baselines[sess.ID] = b
	}
	if prev != nil {
		s.attribute(ctx, repo, snap, prev, sessions, baselines, ended)
	}
	if err := s.d.Store.SaveBaseline(ctx, repoKey(repoID), cur); err != nil {
		slog.Warn("store a repository's latest snapshot", "repository", repoID, "err", err)
	}
	for _, sess := range sessions {
		if sess.Status.IsTerminal() && !contains(ended, sess.ID) {
			continue
		}
		s.refreshStats(ctx, sess, snap)
		s.announceList(ctx, sess.ID)
	}
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// sessionsOf are the sessions of the repository's project that run in it.
func (s *Service) sessionsOf(ctx context.Context, repo *domain.Repository) []*domain.Session {
	all, err := s.d.Sessions.Query(ctx, ports.SessionFilter{ProjectID: repo.ProjectID})
	if err != nil {
		return nil
	}
	var out []*domain.Session
	for _, sess := range all {
		if sess.RepositoryID == repo.ID {
			out = append(out, sess)
		}
	}
	return out
}

// attribute finds the branches and pushes since prev that nothing explains and
// stores each as a finding on one session, or on none (ADR 0070).
func (s *Service) attribute(ctx context.Context, repo *domain.Repository, snap *ports.GitBranchSnapshot, prev *ports.BranchBaseline, sessions []*domain.Session, baselines map[string]*ports.BranchBaseline, ended []string) {
	reqs, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{RepositoryID: repo.ID})
	if err != nil {
		return
	}
	explained := map[string]bool{}
	for _, sess := range sessions {
		explained[sess.Branch] = true
	}
	for _, w := range s.d.Workspaces.ListByRepository(repo.ID) {
		explained[w.Branch] = true
	}
	for _, r := range reqs {
		if !answered(r) {
			continue
		}
		switch r.Action {
		case domain.BranchCreate, domain.BranchSwitch:
			explained[r.Branch] = true
		case domain.BranchRename:
			if r.NewName != nil {
				explained[*r.NewName] = true
			}
		}
	}
	// The turns that may have made it: those that ended, and those still going.
	overlap := map[string]bool{}
	for _, id := range ended {
		overlap[id] = true
	}
	for _, sess := range sessions {
		if sess.Status == domain.SessionThinking || sess.Status == domain.SessionWaitingApproval {
			overlap[sess.ID] = true
		}
	}

	var found []finding
	var newBranches []string
	for name := range snap.Branches {
		if _, had := prev.Branches[name]; !had && !explained[name] {
			newBranches = append(newBranches, name)
		}
	}
	sort.Strings(newBranches)
	gone, taken := s.vanished(ctx, sessions, snap, prev, reqs, newBranches)
	for _, name := range newBranches {
		if !taken[name] {
			found = append(found, finding{kind: domain.FindingBranch, branch: name})
		}
	}
	found = append(found, gone...)
	found = append(found, s.bypassedPushes(ctx, repo, snap, prev)...)
	found = append(found, unapprovedRemotes(snap, prev, reqs)...)

	for _, f := range found {
		candidates := sessions
		if f.owners != nil {
			candidates = f.owners
		}
		who := s.evidenceFor(ctx, f, snap, candidates, baselines, overlap)
		fd := &ports.BranchFinding{
			RepositoryID: repo.ID, Branch: f.branch, Kind: f.kind, Remote: f.remote, RemoteBranch: f.remoteBranch,
			Commit: f.commit, NewName: f.newName, FoundAt: s.d.Now(),
		}
		if len(who) == 1 {
			fd.SessionID = who[0]
		} else {
			fd.Candidates = who
		}
		added, err := s.d.Store.AddFinding(ctx, fd)
		if err != nil {
			slog.Warn("store a branch finding", "repository", repo.ID, "branch", f.branch, "err", err)
			continue
		}
		if added {
			s.announceFinding(ctx, repo, fd)
		}
	}
}

// vanished are the branches of prev that are gone and that a session made or
// touched, with no approved delete or rename behind them: each a delete
// finding, or a rename finding when a branch no request explains appeared at
// its commit in the same check (taken names those new branches). A branch the
// session never touched is left alone, and so is a session branch whose
// worktree the harness removed.
func (s *Service) vanished(ctx context.Context, sessions []*domain.Session, snap *ports.GitBranchSnapshot, prev *ports.BranchBaseline, reqs []domain.BranchRequest, newBranches []string) ([]finding, map[string]bool) {
	byName := map[string]map[string]bool{} // branch -> sessions that made or touched it
	touch := func(name, sessionID string) {
		if name == "" {
			return
		}
		if byName[name] == nil {
			byName[name] = map[string]bool{}
		}
		byName[name][sessionID] = true
	}
	approvedAway := map[string]bool{}
	// A branch an approved create made existed even when no check saw it: it
	// went between two checks, with the commit it started at.
	made := map[string]string{}
	for _, r := range reqs {
		if r.Action == domain.BranchCreate && r.State == domain.BranchRequestUsed && r.Branch != "" && r.StartPoint != nil {
			made[r.Branch] = r.StartPoint.Commit
		}
		touch(r.Branch, r.SessionID)
		if r.NewName != nil {
			touch(*r.NewName, r.SessionID)
		}
		if answered(r) && (r.Action == domain.BranchDelete || r.Action == domain.BranchRename) {
			approvedAway[r.Branch] = true
		}
	}
	ids := make([]string, 0, len(sessions))
	removed := map[string]bool{}
	live := map[string]*domain.Session{}
	for _, sess := range sessions {
		ids = append(ids, sess.ID)
		live[sess.ID] = sess
		if s.workspaceOf(ctx, sess) != nil {
			touch(sess.Branch, sess.ID)
		} else {
			removed[sess.Branch] = true // the harness took the worktree and its branch away
		}
	}
	if fs, err := s.d.Store.Findings(ctx, ids); err == nil {
		for _, f := range fs {
			if f.Kind == domain.FindingBranch {
				touch(f.Branch, f.SessionID)
			}
		}
	}
	commitOf := map[string]string{}
	for name, commit := range made {
		commitOf[name] = commit
	}
	for name, commit := range prev.Branches {
		commitOf[name] = commit
	}
	var names []string
	for name := range commitOf {
		if _, there := snap.Branches[name]; !there && !approvedAway[name] && !removed[name] && len(byName[name]) > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	taken := map[string]bool{}
	var out []finding
	for _, name := range names {
		var owners []*domain.Session
		for id := range byName[name] {
			if sess := live[id]; sess != nil {
				owners = append(owners, sess)
			}
		}
		if len(owners) == 0 {
			continue
		}
		sort.Slice(owners, func(i, j int) bool { return owners[i].ID < owners[j].ID })
		f := finding{kind: domain.FindingDelete, branch: name, commit: commitOf[name], owners: owners}
		for _, n := range newBranches {
			if !taken[n] && commitOf[name] != "" && snap.Branches[n] == commitOf[name] {
				f.kind, f.newName = domain.FindingRename, n
				taken[n] = true
				break
			}
		}
		out = append(out, f)
	}
	return out, taken
}

// unapprovedRemotes are the remotes added since prev, or whose URL changed,
// that no approved remote request covers.
func unapprovedRemotes(snap *ports.GitBranchSnapshot, prev *ports.BranchBaseline, reqs []domain.BranchRequest) []finding {
	before := map[string]string{}
	for _, r := range prev.Remotes {
		before[r.Name] = r.URL
	}
	approved := map[string]bool{}
	for _, r := range reqs {
		if !answered(r) || r.Remote == nil {
			continue
		}
		switch r.Action {
		case domain.BranchRemoteAdd, domain.BranchRemoteSetURL:
			approved[r.Remote.Name] = true
		case domain.BranchRemoteRename:
			if r.Remote.NewName != nil {
				approved[*r.Remote.NewName] = true
			}
		}
	}
	var out []finding
	for _, r := range snap.Remotes {
		if url, had := before[r.Name]; (!had || url != r.URL) && !approved[r.Name] {
			out = append(out, finding{kind: domain.FindingRemote, branch: r.Name, remote: domain.RedactURL(r.URL)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].branch < out[j].branch })
	return out
}

// evidenceFor names the sessions the evidence ties the finding to: those whose
// worktree has the branch checked out or whose HEAD history moved to it,
// else the turns that overlapped. A branch that existed when a session started
// is never that session's.
func (s *Service) evidenceFor(ctx context.Context, f finding, snap *ports.GitBranchSnapshot, sessions []*domain.Session, baselines map[string]*ports.BranchBaseline, overlap map[string]bool) []string {
	live := func(sess *domain.Session) bool {
		if f.kind != domain.FindingBranch {
			return true
		}
		_, existed := baselines[sess.ID].Branches[f.branch]
		return !existed
	}
	var named []string
	for _, sess := range sessions {
		if !live(sess) {
			continue
		}
		if s.checkedOut(ctx, sess, f.branch, snap) || s.headMovedTo(ctx, sess, f.branch) || (f.kind == domain.FindingPush && sess.Branch == f.branch) {
			named = append(named, sess.ID)
		}
	}
	if len(named) == 0 {
		for _, sess := range sessions {
			if overlap[sess.ID] && live(sess) {
				named = append(named, sess.ID)
			}
		}
	}
	sort.Strings(named)
	return named
}

func (s *Service) checkedOut(ctx context.Context, sess *domain.Session, branch string, snap *ports.GitBranchSnapshot) bool {
	ws := s.workspaceOf(ctx, sess)
	if ws == nil {
		return false
	}
	dir := realPath(ws.Path)
	for _, w := range snap.Worktrees {
		if w.Branch == branch && realPath(w.Dir) == dir {
			return true
		}
	}
	return false
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// headMovedTo says the session's worktree HEAD history moved onto the branch.
func (s *Service) headMovedTo(ctx context.Context, sess *domain.Session, branch string) bool {
	ws := s.workspaceOf(ctx, sess)
	if ws == nil {
		return false
	}
	log, err := s.d.Evidence.HEADReflog(ctx, ws.Path)
	if err != nil {
		return false
	}
	for _, e := range log {
		if strings.HasSuffix(e.Message, " to "+branch) {
			return true
		}
	}
	return false
}

// bypassedPushes are the remote-tracking refs that moved since prev by a push
// no recorded push explains. A fetch moves them too, and is never one.
func (s *Service) bypassedPushes(ctx context.Context, repo *domain.Repository, snap *ports.GitBranchSnapshot, prev *ports.BranchBaseline) []finding {
	var moved []string
	for ref, oid := range snap.RemoteRefs {
		if prev.RemoteRefs[ref] != oid {
			moved = append(moved, ref)
		}
	}
	if len(moved) == 0 {
		return nil
	}
	updates, err := s.d.Evidence.RemoteRefUpdates(ctx, repo.RootDir)
	if err != nil {
		slog.Warn("read a repository's remote-tracking reflogs", "repository", repo.ID, "err", err)
		return nil
	}
	var recorded []domain.RecordedPush
	if s.d.Pushes != nil {
		var err error
		if recorded, err = s.d.Pushes.PushesIn(ctx, repo.ID, time.Time{}); err != nil {
			// Without the records every push looks like a bypass: flag none this time.
			slog.Warn("read a repository's recorded pushes", "repository", repo.ID, "err", err)
			return nil
		}
	}
	sort.Strings(moved)
	var out []finding
	for _, ref := range moved {
		remote, branch, _ := strings.Cut(ref, "/")
		oid := snap.RemoteRefs[ref]
		var last *ports.GitRemoteRefUpdate
		for i := range updates { // newest first
			if updates[i].Remote == remote && updates[i].Branch == branch && updates[i].New == oid {
				last = &updates[i]
				break
			}
		}
		if last == nil || !last.Push {
			continue
		}
		if isRecorded(recorded, remote, branch, oid) {
			continue
		}
		out = append(out, finding{kind: domain.FindingPush, branch: branch, remote: remote, remoteBranch: branch, commit: oid})
	}
	return out
}

func isRecorded(rs []domain.RecordedPush, remote, branch, commit string) bool {
	for _, r := range rs {
		if r.Remote == remote && r.RemoteBranch == branch && r.Commit == commit {
			return true
		}
	}
	return false
}

// announceFinding tells the inbox and the project feed about a new finding.
func (s *Service) announceFinding(ctx context.Context, repo *domain.Repository, f *ports.BranchFinding) {
	ev := domain.BranchFindingFound{
		ProjectID: repo.ProjectID, SessionID: f.SessionID, RepositoryID: repo.ID, Branch: f.Branch,
		Kind: f.Kind, NewName: f.NewName, URL: remoteURL(f), Candidates: f.Candidates, FoundAt: f.FoundAt,
	}
	if sess := s.d.Sessions.Get(f.SessionID); sess != nil {
		ev.TaskID = sess.TicketID
	}
	if s.d.Events != nil {
		if err := s.d.Events.Publish(ctx, ev); err != nil {
			slog.Warn("announce a branch finding", "branch", f.Branch, "err", err)
		}
	}
	if f.SessionID == "" && s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{UnattributedBranch: &ports.UnattributedBranchChange{
			UnattributedBranch: domain.UnattributedBranch{
				RepositoryID: repo.ID, Branch: f.Branch, Kind: string(f.Kind), CandidateSessionIDs: f.Candidates, FoundAt: f.FoundAt,
			}, ProjectID: repo.ProjectID,
		}})
	}
}

// refreshStats counts the session's branches that exist against their bases.
func (s *Service) refreshStats(ctx context.Context, sess *domain.Session, snap *ports.GitBranchSnapshot) {
	reqs, err := s.d.Requests.List(ctx, domain.BranchRequestFilter{SessionID: sess.ID})
	if err != nil {
		return
	}
	in, err := s.inputs(ctx, []*domain.Session{sess}, map[string][]domain.BranchRequest{sess.ID: reqs})
	if err != nil {
		return
	}
	dir := s.divergenceDir(ctx, sess)
	bases := map[string]string{sess.Branch: in.baseRef[sess.ID]}
	for _, r := range reqs {
		if r.Action == domain.BranchCreate && r.StartPoint != nil && r.Branch != "" && bases[r.Branch] == "" {
			ref := r.StartPoint.Ref
			if r.StartPoint.Kind == "commit" && r.StartPoint.Commit != "" {
				ref = r.StartPoint.Commit
			}
			bases[r.Branch] = ref
		}
	}
	var stats []ports.BranchStat
	for _, b := range in.build(sess) {
		base := bases[b.Name]
		_, exists := snap.Branches[b.Name]
		if base == "" || !exists || b.State == domain.SessionBranchDeleted || b.State == domain.SessionBranchDenied {
			continue
		}
		ahead, behind, err := s.d.Git.Divergence(ctx, dir, base, b.Name)
		if err != nil {
			continue
		}
		stats = append(stats, ports.BranchStat{SessionID: sess.ID, Branch: b.Name, BaseRef: base, Ahead: ahead, Behind: behind})
	}
	if err := s.d.Store.SaveStats(ctx, sess.ID, stats); err != nil {
		slog.Warn("store a session's branch counts", "session", sess.ID, "err", err)
	}
}

// divergenceDir is where the counts are read: the worktree, or the
// repository's checkout when the worktree is gone.
func (s *Service) divergenceDir(ctx context.Context, sess *domain.Session) string {
	if ws := s.workspaceOf(ctx, sess); ws != nil && ws.Path != "" {
		if _, err := filepath.Abs(ws.Path); err == nil {
			if fi, err := os.Stat(ws.Path); err == nil && fi.IsDir() {
				return ws.Path
			}
		}
	}
	if repo := s.d.Repositories.Get(ctx, sess.RepositoryID); repo != nil {
		return repo.RootDir
	}
	return ""
}

// remoteURL is the redacted URL a remote finding names, "" for any other.
func remoteURL(f *ports.BranchFinding) string {
	if f.Kind != domain.FindingRemote {
		return ""
	}
	return f.Remote
}

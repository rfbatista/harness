package workspaces

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchMerger = (*Merges)(nil)

// MergeDeps is what the merge use case needs. Announce may be nil.
type MergeDeps struct {
	Sessions     ports.SessionRepository
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	Git          ports.GitMerger
	Announce     ports.SessionChangeAnnouncer
	// States hears that a session's code landed, so the task's merge state is
	// read again at once; may be nil.
	States ports.ProjectChangeSink
	// Now is the clock results are stamped with; nil is time.Now.
	Now func() time.Time
}

// Merges merges a session's branch into its repository's default branch in the
// repository's local checkout (ADR 0021). The harness server runs it, so no
// agent needs write access to a main checkout. It never pushes, never
// restarts anything and never moves the task's status (ADR 0013).
type Merges struct {
	d MergeDeps

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per checkout: one merge at a time
}

// NewMerges returns the use case.
func NewMerges(d MergeDeps) *Merges {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Merges{d: d, locks: map[string]*sync.Mutex{}}
}

// lock serialises merges into one checkout.
func (s *Merges) lock(root string) func() {
	s.mu.Lock()
	l, ok := s.locks[root]
	if !ok {
		l = &sync.Mutex{}
		s.locks[root] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// mergeTarget is a session with where its branch merges.
type mergeTarget struct {
	sess *domain.Session
	root string
	base string // the branch the workspace was cut from; "" when it is none
	// worktree is the session's worktree directory; "" when it has none.
	worktree string
}

// target resolves the session, its branch and its repository's local
// checkout, or says which of them is missing. SESSION_NOT_FOUND is the one
// error that is not a recorded refusal.
func (s *Merges) target(ctx context.Context, sessionID string) (*mergeTarget, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "session_id is required"}
	}
	sess, err := s.d.Sessions.Find(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	t := &mergeTarget{sess: sess}
	if sess.Branch == "" {
		return t, domain.MergeError(domain.MergeNoBranch, "this session has no branch to merge")
	}
	repo := s.d.Repositories.Get(ctx, sess.RepositoryID)
	if repo == nil || strings.TrimSpace(repo.RootDir) == "" {
		return t, domain.MergeError(domain.MergeNoCheckout, "no checkout to merge in: the session's repository has no local checkout")
	}
	t.root = domain.ExpandUserPath(repo.RootDir)
	if sess.WorkspaceID != "" {
		if ws := s.d.Workspaces.Get(ctx, sess.WorkspaceID); ws != nil {
			t.base = domain.BranchFromRef(ws.BaseRef)
			t.worktree = ws.Path
		}
	}
	return t, nil
}

// MergeOffer answers what the Merge tab shows for a merge of the session's
// branch into target ("" is the default target): whether it can be attempted,
// why not, how far ahead the branch is, what the session's worktree has
// uncommitted, a squash message to start from, and every local branch the
// branch could go into. It reads local git and the stored session only, in
// four git runs however many branches there are.
func (s *Merges) MergeOffer(ctx context.Context, sessionID, target string) (*ports.MergeOffer, error) {
	t, err := s.target(ctx, sessionID)
	offer := &ports.MergeOffer{Into: target, Targets: []ports.MergeTarget{}}
	if t != nil && t.sess != nil {
		offer.Branch = t.sess.Branch
	}
	if code := errs.Code(err); code == domain.MergeNoBranch || code == domain.MergeNoCheckout {
		offer.Reason = code
		return offer, nil
	} else if err != nil {
		return nil, err
	}
	st, err := s.d.Git.Checkout(ctx, t.root)
	if err != nil {
		offer.Reason = errs.Code(err)
		return offer, nil
	}
	bl, err := s.d.Git.Branches(ctx, t.root)
	if err != nil {
		return nil, err
	}
	offer.Targets = mergeTargets(bl, st, t.sess.Branch)
	into := target
	if into == "" {
		into = defaultTarget(t, st, bl)
	}
	offer.Into = into

	if t.worktree != "" {
		if wt, err := s.d.Git.Checkout(ctx, t.worktree); err == nil {
			offer.UncommittedChanges = len(wt.Tracked) + len(wt.Untracked)
		}
	}
	offer.SuggestedMessage = strings.TrimSpace(t.sess.Task)
	if offer.SuggestedMessage == "" && slices.Contains(bl.Names, t.sess.Branch) {
		offer.SuggestedMessage, _ = s.d.Git.Subject(ctx, t.root, t.sess.Branch)
	}

	switch {
	case !slices.Contains(bl.Names, t.sess.Branch):
		offer.Reason = domain.MergeBranchNotFound
	case into == "" || into == t.sess.Branch || !slices.Contains(bl.Names, into):
		offer.Reason = domain.MergeTargetNotFound
	case into == st.Branch && !st.Clean():
		offer.Reason = domain.MergeCheckoutDirty
	default:
		offer.Ahead, err = s.d.Git.Ahead(ctx, t.root, into, t.sess.Branch)
		switch {
		case err != nil:
			offer.Reason = errs.Code(err)
		case offer.Ahead == 0:
			offer.Reason = domain.MergeAlreadyMerged
		default:
			offer.Available = true
		}
	}
	return offer, nil
}

// defaultTarget is where a merge goes when the person names no target: the
// branch the workspace was cut from, when it is still a local branch, else the
// checked-out branch, else the repository's default branch.
func defaultTarget(t *mergeTarget, st ports.CheckoutState, bl ports.BranchList) string {
	for _, b := range []string{t.base, st.Branch, bl.Default} {
		if b != "" && slices.Contains(bl.Names, b) {
			return b
		}
	}
	return ""
}

// mergeTargets lists every local branch but the session's own: the default
// branch first, then by name.
func mergeTargets(bl ports.BranchList, st ports.CheckoutState, own string) []ports.MergeTarget {
	out := []ports.MergeTarget{}
	for _, name := range bl.Names {
		if name == own {
			continue
		}
		out = append(out, ports.MergeTarget{
			Name: name, Default: name == bl.Default,
			CheckedOut: name == st.Branch, Dirty: name == st.Branch && !st.Clean(),
		})
	}
	slices.SortFunc(out, func(a, b ports.MergeTarget) int {
		switch {
		case a.Default != b.Default:
			if a.Default {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// BranchChangedSince says whether the session's branch gained a commit, not on
// the default branch, committed after since. The default branch is the one the
// workspace was cut from, else the one the checkout has checked out.
func (s *Merges) BranchChangedSince(ctx context.Context, sessionID string, since time.Time) (bool, error) {
	t, err := s.target(ctx, sessionID)
	if err != nil {
		return false, err
	}
	into := t.base
	if into == "" {
		st, err := s.d.Git.Checkout(ctx, t.root)
		if err != nil {
			return false, err
		}
		into = st.Branch
	}
	if into == "" {
		return false, errors.New("the default branch is not known")
	}
	return s.d.Git.BranchCommittedAfter(ctx, t.root, t.sess.Branch, into, since)
}

// MergeSessionBranch merges the session's branch with --no-ff into the
// repository's default branch, and records how it ended on the session: the
// new HEAD, or the refusal. The default branch is the one the workspace was
// cut from when that branch exists in the checkout, else the one the checkout
// has checked out.
func (s *Merges) MergeSessionBranch(ctx context.Context, sessionID string) (*domain.MergeResult, error) {
	// A merge that was started finishes (or is undone): the caller going away
	// does not leave a half merge. Every git run is bounded by its deadline.
	ctx = context.WithoutCancel(ctx)
	t, err := s.target(ctx, sessionID)
	if err != nil {
		if t != nil && t.sess != nil {
			return nil, s.refuse(t.sess.ID, "", domain.MergeMethodMerge, err)
		}
		return nil, err
	}
	defer s.lock(t.root)()

	st, err := s.d.Git.Checkout(ctx, t.root)
	if err != nil {
		return nil, s.refuse(sessionID, t.base, domain.MergeMethodMerge, err)
	}
	into := t.base
	if into == "" || into != st.Branch && !s.d.Git.HasBranch(ctx, t.root, into) {
		into = st.Branch
	}
	switch {
	case into == "":
		return nil, s.refuse(sessionID, "", domain.MergeMethodMerge, domain.MergeError(domain.MergeWrongBranch,
			"the checkout at "+t.root+" is on a detached HEAD, not on the default branch; check the default branch out there first"))
	case st.Branch != into:
		return nil, s.refuse(sessionID, into, domain.MergeMethodMerge, domain.MergeError(domain.MergeWrongBranch,
			"the checkout at "+t.root+" is on "+branchWord(st.Branch)+", not on the default branch "+into+"; check "+into+" out there first"))
	case !st.Clean():
		return nil, s.refuse(sessionID, into, domain.MergeMethodMerge, domain.MergeError(domain.MergeCheckoutDirty,
			"the checkout at "+t.root+" is not clean: "+dirtyWords(st)+"; commit, stash or remove them first"))
	}

	head, err := s.d.Git.Merge(ctx, t.root, t.sess.Branch)
	if err != nil {
		return nil, s.refuse(sessionID, into, domain.MergeMethodMerge, err)
	}
	res := domain.MergeResult{At: s.d.Now(), Merged: true, Into: into, Method: domain.MergeMethodMerge, Head: head}
	if err := s.record(sessionID, res); err != nil {
		return nil, err
	}
	return &res, nil
}

// MergeInto merges the session's branch into the target the person picked,
// with the method they picked, and records how it ended on the session, as a
// refusal too. A target that is not checked out in the main checkout is merged
// without touching it; one that is checked out must be clean and has its files
// updated. Only an invalid request and an unknown session are not recorded.
func (s *Merges) MergeInto(ctx context.Context, sessionID string, req ports.MergeRequest) (*domain.MergeResult, error) {
	method := req.Method
	if method == "" {
		method = domain.MergeMethodMerge
	}
	message := strings.TrimSpace(req.Message)
	switch {
	case method != domain.MergeMethodMerge && method != domain.MergeMethodSquash:
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "method must be \"merge\" or \"squash\""}
	case method == domain.MergeMethodSquash && message == "":
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "a squash needs a message"}
	}
	// A merge that was started finishes (or is undone): the caller going away
	// does not leave a half merge.
	ctx = context.WithoutCancel(ctx)
	t, err := s.target(ctx, sessionID)
	if err != nil {
		if t != nil && t.sess != nil {
			return nil, s.refuse(t.sess.ID, req.Target, method, err)
		}
		return nil, err
	}
	defer s.lock(t.root)()

	st, err := s.d.Git.Checkout(ctx, t.root)
	if err != nil {
		return nil, s.refuse(sessionID, req.Target, method, err)
	}
	bl, err := s.d.Git.Branches(ctx, t.root)
	if err != nil {
		return nil, s.refuse(sessionID, req.Target, method, err)
	}
	into := req.Target
	if into == "" {
		into = defaultTarget(t, st, bl)
	}
	switch {
	case !slices.Contains(bl.Names, t.sess.Branch):
		return nil, s.refuse(sessionID, into, method, domain.MergeError(domain.MergeBranchNotFound, "the branch "+t.sess.Branch+" no longer exists"))
	case into == "" || into == t.sess.Branch || !slices.Contains(bl.Names, into):
		return nil, s.refuse(sessionID, into, method, domain.MergeError(domain.MergeTargetNotFound,
			domain.MergeTargetNotFound+": "+targetWords(into)+" is not a local branch the session's branch can be merged into"))
	}
	checkedOut := st.Branch == into
	if checkedOut && !st.Clean() {
		return nil, s.refuse(sessionID, into, method, domain.MergeError(domain.MergeCheckoutDirty,
			"the checkout at "+t.root+" is not clean: "+dirtyWords(st)+"; commit, stash or remove them first"))
	}

	var head string
	if checkedOut && method == domain.MergeMethodMerge {
		head, err = s.d.Git.Merge(ctx, t.root, t.sess.Branch)
	} else {
		head, err = s.d.Git.MergeInto(ctx, t.root, into, t.sess.Branch, method, message, checkedOut)
	}
	if err != nil {
		return nil, s.refuse(sessionID, into, method, err)
	}
	res := domain.MergeResult{At: s.d.Now(), Merged: true, Into: into, Method: method, Head: head,
		SquashedIntoDefault: method == domain.MergeMethodSquash && into == bl.Default}
	if err := s.record(sessionID, res); err != nil {
		return nil, err
	}
	return &res, nil
}

func targetWords(into string) string {
	if into == "" {
		return "no target"
	}
	return "the target " + into
}

// refuse records err as the session's newest merge result, announces it, and
// answers it. A refusal that carries no code is a failed merge.
func (s *Merges) refuse(sessionID, into string, method domain.MergeMethod, err error) error {
	code := errs.Code(err)
	if !strings.HasPrefix(code, "MERGE_") {
		code = domain.MergeFailed
	}
	res := domain.MergeResult{At: s.d.Now(), Into: into, Method: method, Code: code, Message: messageOf(err)}
	if rerr := s.record(sessionID, res); rerr != nil {
		return rerr
	}
	return err
}

func (s *Merges) record(sessionID string, res domain.MergeResult) error {
	if err := s.d.Sessions.UpdateMerge(sessionID, res); err != nil {
		return err
	}
	if s.d.Announce != nil {
		s.d.Announce.AnnounceSession(sessionID)
	}
	if s.d.States != nil && res.Merged {
		if sess, err := s.d.Sessions.Find(context.Background(), sessionID); err == nil && sess != nil && sess.WorkspaceID != "" {
			s.d.States.AnnounceChange(ports.ProjectChange{CodeChanged: &ports.CodeChange{ProjectID: sess.ProjectID, WorkspaceID: sess.WorkspaceID}})
		}
	}
	return nil
}

func branchWord(b string) string {
	if b == "" {
		return "a detached HEAD"
	}
	return b
}

// dirtyWords says which kind of uncommitted change is in the way, and where.
func dirtyWords(st ports.CheckoutState) string {
	var parts []string
	if len(st.Tracked) > 0 {
		parts = append(parts, "uncommitted changes to tracked files ("+pathList(st.Tracked)+")")
	}
	if len(st.Untracked) > 0 {
		parts = append(parts, "untracked files ("+pathList(st.Untracked)+")")
	}
	return strings.Join(parts, " and ")
}

func pathList(paths []string) string {
	const max = 5
	if len(paths) <= max {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:max], ", ") + ", and " + strconv.Itoa(len(paths)-max) + " more"
}

// messageOf is err's message without its code.
func messageOf(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Message
	}
	return err.Error()
}

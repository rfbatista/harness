package workspaces

import (
	"context"
	"errors"
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
		}
	}
	return t, nil
}

// MergeOffer says whether a merge applies to the session and, when it is
// known without asking git, which branch it goes into. It spawns git only
// when the workspace was cut from no branch.
func (s *Merges) MergeOffer(ctx context.Context, sessionID string) (*ports.MergeOffer, error) {
	t, err := s.target(ctx, sessionID)
	if code := errs.Code(err); code == domain.MergeNoBranch || code == domain.MergeNoCheckout {
		return &ports.MergeOffer{Reason: code}, nil
	} else if err != nil {
		return nil, err
	}
	into := t.base
	if into == "" {
		if st, err := s.d.Git.Checkout(ctx, t.root); err != nil {
			return &ports.MergeOffer{Reason: errs.Code(err)}, nil
		} else {
			into = st.Branch
		}
	}
	return &ports.MergeOffer{Available: true, Into: into}, nil
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
			return nil, s.refuse(t.sess.ID, "", err)
		}
		return nil, err
	}
	defer s.lock(t.root)()

	st, err := s.d.Git.Checkout(ctx, t.root)
	if err != nil {
		return nil, s.refuse(sessionID, t.base, err)
	}
	into := t.base
	if into == "" || into != st.Branch && !s.d.Git.HasBranch(ctx, t.root, into) {
		into = st.Branch
	}
	switch {
	case into == "":
		return nil, s.refuse(sessionID, "", domain.MergeError(domain.MergeWrongBranch,
			"the checkout at "+t.root+" is on a detached HEAD, not on the default branch; check the default branch out there first"))
	case st.Branch != into:
		return nil, s.refuse(sessionID, into, domain.MergeError(domain.MergeWrongBranch,
			"the checkout at "+t.root+" is on "+branchWord(st.Branch)+", not on the default branch "+into+"; check "+into+" out there first"))
	case !st.Clean():
		return nil, s.refuse(sessionID, into, domain.MergeError(domain.MergeCheckoutDirty,
			"the checkout at "+t.root+" is not clean: "+dirtyWords(st)+"; commit, stash or remove them first"))
	}

	head, err := s.d.Git.Merge(ctx, t.root, t.sess.Branch)
	if err != nil {
		return nil, s.refuse(sessionID, into, err)
	}
	res := domain.MergeResult{At: s.d.Now(), Merged: true, Into: into, Head: head}
	if err := s.record(sessionID, res); err != nil {
		return nil, err
	}
	return &res, nil
}

// refuse records err as the session's newest merge result, announces it, and
// answers it. A refusal that carries no code is a failed merge.
func (s *Merges) refuse(sessionID, into string, err error) error {
	code := errs.Code(err)
	if !strings.HasPrefix(code, "MERGE_") {
		code = domain.MergeFailed
	}
	res := domain.MergeResult{At: s.d.Now(), Into: into, Code: code, Message: messageOf(err)}
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

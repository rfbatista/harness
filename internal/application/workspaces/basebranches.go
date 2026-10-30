package workspaces

import (
	"context"
	"errors"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BaseBranchPreparer = (*BaseBranches)(nil)

const originRemote = "origin"

// BaseBranches readies the base a session's branch is cut from (ADR 0049). It
// judges the base from the branch list the cut itself reads (refs/heads and
// refs/remotes), so the classification and the listing cannot disagree; the
// one name that may be absent from it is origin's, which the fetch settles. A
// name that is neither listed nor origin's never reaches git worktree add.
type BaseBranches struct {
	worktrees ports.WorktreeManager
	git       ports.BranchPublisher
	// fetcher is nil in a graph without the git credentials context: a base of
	// origin's then cannot be fetched, and is refused as unavailable.
	fetcher *OriginFetcher
}

// NewBaseBranches returns the preparer.
func NewBaseBranches(worktrees ports.WorktreeManager, git ports.BranchPublisher, fetcher *OriginFetcher) *BaseBranches {
	return &BaseBranches{worktrees: worktrees, git: git, fetcher: fetcher}
}

// PrepareBase reads the branch list once, and only for a base that is named.
func (s *BaseBranches) PrepareBase(ctx context.Context, repositoryID, repoRoot, base string) error {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil
	}
	branches, err := s.worktrees.ListBranches(repoRoot)
	if err != nil {
		return err
	}
	var listed *domain.GitBranch
	for i, b := range branches {
		if b.Name == base {
			listed = &branches[i]
			break // a local branch is listed before the remote ones
		}
	}
	// A base named origin/<name> is always tried on origin, listed or not: an
	// architect has no fetch of its own, and a branch nobody fetched yet is
	// exactly what it may ask for. The fetch lands it or says it is gone.
	fromOrigin := strings.HasPrefix(base, originRemote+"/") && len(base) > len(originRemote)+1
	switch {
	case listed != nil && !listed.Remote:
		return nil
	case !fromOrigin && listed == nil:
		return baseNotFound(base)
	case !fromOrigin:
		return nil // another remote's ref: cut as it is
	}
	branch := strings.TrimPrefix(base, originRemote+"/")

	origin, err := s.git.OriginURL(ctx, repoRoot)
	if err != nil {
		return err
	}
	if origin == "" {
		return &domain.StructuredError{Code: domain.CodeNoRemote, Message: "the repository has no origin remote, so " + base + " cannot be fetched"}
	}
	if s.fetcher == nil {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "fetching " + base + " is not available"}
	}
	err = s.fetcher.FetchBranch(ctx, repositoryID, repoRoot, origin, branch)
	var failed *OriginFetchError
	switch {
	case errors.Is(err, ports.ErrRemoteBranchNotFound):
		return remoteBranchGone(base, branch)
	case errors.As(err, &failed):
		return &domain.StructuredError{Code: domain.CodeFetchFailed, Message: "fetching " + base + " failed: " + failed.Error()}
	}
	return err
}

func baseNotFound(base string) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.BaseBranchNotFound,
			Message: base + " is not a branch of this repository. Pick one from the list, or fetch from origin to refresh it."},
		Details: map[string]string{"base_branch": base},
	}
}

func remoteBranchGone(base, branch string) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: domain.RemoteBranchNotFound,
			Message: base + " is not on origin any more. Fetch from origin to refresh the list."},
		Details: map[string]string{"base_branch": base, "remote": originRemote, "branch": branch},
	}
}

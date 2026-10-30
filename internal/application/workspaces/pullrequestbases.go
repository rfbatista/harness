package workspaces

import (
	"context"
	"errors"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.PullRequestBases = (*PullRequests)(nil)

// PullRequestBases answers the branches a workspace's pull request can be
// opened against, from local git only: origin's as last fetched, and when. It
// never fetches, so a workspace with no origin has none, not an error.
func (s *PullRequests) PullRequestBases(ctx context.Context, workspaceID string) (*domain.PullRequestBases, error) {
	t, err := s.resolve(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return s.bases(ctx, t)
}

func (s *PullRequests) bases(ctx context.Context, t *target) (*domain.PullRequestBases, error) {
	out := &domain.PullRequestBases{WorkspaceID: t.ws.ID, Branches: []string{}, Default: t.base}
	if t.origin == "" {
		return out, nil
	}
	branches, fetchedAt, err := s.d.Git.OriginBranches(ctx, t.root)
	if err != nil {
		return nil, err
	}
	out.Branches = branches
	if !fetchedAt.IsZero() {
		out.FetchedAt = &fetchedAt
	}
	return out, nil
}

// RefreshPullRequestBases fetches origin, with --prune, using the stored
// credential that pull request create uses, and answers the new list. One
// refresh per repository runs at a time. It never touches the main checkout's
// files or branch, and a failure leaves the list GET reads as it was.
func (s *PullRequests) RefreshPullRequestBases(ctx context.Context, workspaceID string) (*domain.PullRequestBases, error) {
	t, err := s.resolve(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	switch {
	case t.remote == nil:
		return nil, domain.PullRequestError(domain.CodePRNoRemote, "the repository has no origin remote on GitHub")
	case !t.remote.IsGitHub():
		return nil, domain.PullRequestError(domain.CodePRNoRemote, "origin is on "+t.remote.Host+", not GitHub; pull requests are opened on GitHub only")
	}
	if err := s.missingCredential(ctx, t); err != nil {
		return nil, err
	}
	err = s.fetcher.Fetch(ctx, t.ws.RepositoryID, t.root, t.origin)
	var failed *OriginFetchError
	if errors.As(err, &failed) {
		return nil, domain.PullRequestError(domain.CodePRFetchFailed, "fetching origin failed: "+failed.Error())
	} else if err != nil {
		return nil, err
	}
	return s.bases(ctx, t)
}

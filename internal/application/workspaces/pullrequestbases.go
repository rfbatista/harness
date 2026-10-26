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
	lock := s.lock("fetch\x00" + t.root)
	lock.Lock()
	defer lock.Unlock()

	if err := s.missingCredential(ctx, t); err != nil {
		return nil, err
	}
	op, err := s.d.Auth.Operation(ctx, t.ws.RepositoryID, t.origin)
	if err != nil {
		return nil, err
	}
	defer op.Release()
	if op.Push == nil {
		return nil, s.missingCredential(ctx, t)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	err = s.d.Git.FetchOrigin(fetchCtx, t.root, op.Env)
	cancel()
	switch {
	case errors.Is(err, ports.ErrRemoteAuthRejected):
		return nil, domain.GitCredentialRejected(op.Push.Host, op.Push.ID, reasonAfter(err, ports.ErrRemoteAuthRejected))
	case err != nil:
		return nil, domain.PullRequestError(domain.CodePRFetchFailed, "fetching origin failed: "+err.Error())
	}
	s.d.Auth.Used(op.Push.ID)
	return s.bases(ctx, t)
}

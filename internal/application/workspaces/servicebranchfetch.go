package workspaces

import (
	"context"
	"errors"
	"strconv"

	"operators-mcp/internal/ports"
)

var _ ports.ServiceBranchFetcher = (*ServiceBranchFetch)(nil)

// ServiceBranchFetch fetches the one branch a service run on a remote branch
// needs (ADR 0080), through the same credential-aware, per-repository fetch
// as a session started from origin/<name> (ADR 0049).
type ServiceBranchFetch struct {
	git     ports.BranchPublisher
	fetcher *OriginFetcher
}

// NewServiceBranchFetch returns the fetch over the origin fetcher.
func NewServiceBranchFetch(git ports.BranchPublisher, fetcher *OriginFetcher) *ServiceBranchFetch {
	return &ServiceBranchFetch{git: git, fetcher: fetcher}
}

// FetchServiceBranch updates refs/remotes/origin/<branch> in the checkout at
// root. Every error is worded for a run's `error`: git's reason with URLs
// redacted, never a credential.
func (f *ServiceBranchFetch) FetchServiceBranch(ctx context.Context, repositoryID, root, branch string) error {
	origin, err := f.git.OriginURL(ctx, root)
	if err != nil {
		return err
	}
	if origin == "" {
		return errors.New("the repository has no origin remote, so branch " + strconv.Quote(branch) + " cannot be fetched")
	}
	err = f.fetcher.FetchBranch(ctx, repositoryID, root, origin, branch)
	if errors.Is(err, ports.ErrRemoteBranchNotFound) {
		return errors.New("branch " + strconv.Quote(branch) + " is not on origin any more")
	}
	return err
}

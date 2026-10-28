package workspaces

import (
	"context"
	"errors"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// OriginFetchTimeout bounds one fetch of a repository's origin.
const OriginFetchTimeout = 60 * time.Second

// OriginFetchError is a fetch that failed for a reason other than the
// credential: Err carries git's reason. Each caller words it in its own code.
type OriginFetchError struct{ Err error }

func (e *OriginFetchError) Error() string { return e.Err.Error() }
func (e *OriginFetchError) Unwrap() error { return e.Err }

// OriginFetcher fetches a repository's origin with the stored credential:
// every refs/remotes/origin/* is updated and pruned, nothing in a working tree
// or local branch moves. It is the one fetch the pull request base refresh and
// the worktree git state share, so either updates the other's data. One fetch
// per repository runs at a time, and observers hear each one that succeeded.
type OriginFetcher struct {
	git     ports.BranchPublisher
	auth    ports.GitAuth
	timeout time.Duration

	mu        sync.Mutex
	locks     map[string]*sync.Mutex // per repository checkout
	observers []func(ctx context.Context, repositoryID string)
}

// NewOriginFetcher returns the fetch over git and the credential store.
func NewOriginFetcher(git ports.BranchPublisher, auth ports.GitAuth) *OriginFetcher {
	return &OriginFetcher{git: git, auth: auth, timeout: OriginFetchTimeout, locks: map[string]*sync.Mutex{}}
}

// WithTimeout bounds each fetch to d instead of OriginFetchTimeout.
func (f *OriginFetcher) WithTimeout(d time.Duration) *OriginFetcher {
	f.timeout = d
	return f
}

// OnFetched registers fn to run, after the lock is released, for each
// repository whose origin was fetched.
func (f *OriginFetcher) OnFetched(fn func(ctx context.Context, repositoryID string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observers = append(f.observers, fn)
}

func (f *OriginFetcher) lock(root string) *sync.Mutex {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.locks[root]
	if !ok {
		l = &sync.Mutex{}
		f.locks[root] = l
	}
	return l
}

// Fetch fetches the origin of the repository whose checkout is root. A remote
// that needs a credential and has none stored is GIT_CREDENTIAL_MISSING; one
// that refuses it, GIT_CREDENTIAL_REJECTED; a remote that needs none (a file
// path) is fetched without one. Any other failure is an *OriginFetchError.
func (f *OriginFetcher) Fetch(ctx context.Context, repositoryID, root, originURL string) error {
	if err := f.fetch(ctx, repositoryID, root, originURL); err != nil {
		return err
	}
	f.mu.Lock()
	observers := append([]func(context.Context, string){}, f.observers...)
	f.mu.Unlock()
	for _, fn := range observers {
		fn(ctx, repositoryID)
	}
	return nil
}

// credential opens the credentials of one remote operation. A remote that
// needs one and has none stored is GIT_CREDENTIAL_MISSING; a remote that needs
// none (a file path) is served without one. The caller releases the operation.
func (f *OriginFetcher) credential(ctx context.Context, repositoryID, originURL string) (*ports.GitOperation, error) {
	op, err := f.auth.Operation(ctx, repositoryID, originURL)
	if err != nil {
		return nil, err
	}
	if op.Push == nil {
		res, err := f.auth.Resolve(ctx, repositoryID, originURL)
		if err != nil {
			op.Release()
			return nil, err
		}
		if res.Remote != nil && res.Remote.Kind != "" {
			op.Release()
			return nil, domain.GitCredentialMissing(res.Remote.Host, res.Remote.Kind)
		}
	}
	return op, nil
}

// refused is the error a remote's refusal of the credential is.
func refused(op *ports.GitOperation, err error) error {
	host, id := "", ""
	if op.Push != nil {
		host, id = op.Push.Host, op.Push.ID
	}
	return domain.GitCredentialRejected(host, id, reasonAfter(err, ports.ErrRemoteAuthRejected))
}

func (f *OriginFetcher) fetch(ctx context.Context, repositoryID, root, originURL string) error {
	lock := f.lock(root)
	lock.Lock()
	defer lock.Unlock()

	op, err := f.credential(ctx, repositoryID, originURL)
	if err != nil {
		return err
	}
	defer op.Release()
	fetchCtx, cancel := context.WithTimeout(ctx, f.timeout)
	err = f.git.FetchOrigin(fetchCtx, root, op.Env)
	cancel()
	switch {
	case errors.Is(err, ports.ErrRemoteAuthRejected):
		return refused(op, err)
	case err != nil:
		return &OriginFetchError{Err: err}
	}
	if op.Push != nil {
		f.auth.Used(op.Push.ID)
	}
	return nil
}

// OriginPushError is a push that failed for a reason other than the
// credential. Rejected says the remote refused what was pushed (not a
// fast-forward, a hook); Err carries git's reason.
type OriginPushError struct {
	Err      error
	Rejected bool
}

func (e *OriginPushError) Error() string { return e.Err.Error() }
func (e *OriginPushError) Unwrap() error { return e.Err }

// Push publishes branch to origin with the stored credential and sets its
// upstream; never forced. Credential refusals are as Fetch's, any other
// failure an *OriginPushError.
func (f *OriginFetcher) Push(ctx context.Context, repositoryID, root, originURL, branch string) error {
	op, err := f.credential(ctx, repositoryID, originURL)
	if err != nil {
		return err
	}
	defer op.Release()
	pushCtx, cancel := context.WithTimeout(ctx, f.timeout)
	err = f.git.Push(pushCtx, root, branch, op.Env)
	cancel()
	switch {
	case errors.Is(err, ports.ErrRemoteAuthRejected):
		return refused(op, err)
	case err != nil:
		return &OriginPushError{Err: err, Rejected: errors.Is(err, ports.ErrPushRejected)}
	}
	if op.Push != nil {
		f.auth.Used(op.Push.ID)
	}
	return nil
}

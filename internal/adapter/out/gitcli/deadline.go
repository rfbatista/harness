package gitcli

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultWorktreeDeadline bounds one git run that adds, removes or prunes
	// a worktree, or deletes its branch: they write a checkout's worth of
	// files, so they get longer than a read.
	DefaultWorktreeDeadline = 60 * time.Second
	// DefaultReadDeadline bounds one git run that only reads a repository.
	DefaultReadDeadline = 10 * time.Second
	// waitDelay is how long a killed git's output pipes are still read after
	// the kill: a helper git started (ssh, a credential helper, a hook)
	// inherits them and may outlive git, which would otherwise hold Wait.
	waitDelay = 2 * time.Second
)

// Deadlines bound how long each git run waits before it is killed and its
// operation fails. Push and fetch are not bound by them: their callers give
// them their own, longer deadline. A zero field uses its default.
type Deadlines struct {
	Worktree time.Duration
	Read     time.Duration
}

// Option configures an adapter's Deadlines.
type Option func(*Deadlines)

// WithWorktreeDeadline bounds each worktree add, remove and prune (and branch
// delete) at d. Zero or less keeps DefaultWorktreeDeadline.
func WithWorktreeDeadline(d time.Duration) Option {
	return func(ds *Deadlines) { ds.Worktree = d }
}

// WithReadDeadline bounds each read-only git run at d. Zero or less keeps
// DefaultReadDeadline.
func WithReadDeadline(d time.Duration) Option {
	return func(ds *Deadlines) { ds.Read = d }
}

func newDeadlines(opts []Option) Deadlines {
	var d Deadlines
	for _, o := range opts {
		o(&d)
	}
	return d
}

func (d Deadlines) worktree() time.Duration {
	if d.Worktree <= 0 {
		return DefaultWorktreeDeadline
	}
	return d.Worktree
}

func (d Deadlines) read() time.Duration {
	if d.Read <= 0 {
		return DefaultReadDeadline
	}
	return d.Read
}

// cut is err, unless ctx's deadline fired first: then it is an error that
// says git gave no answer in time and wraps context.DeadlineExceeded,
// whatever exit the kill produced.
func cut(ctx context.Context, what string, err error) error {
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("git %s: no answer in time: %w", what, context.DeadlineExceeded)
	}
	return err
}

// timedOut reports whether err is a git run cut by a deadline.
func timedOut(err error) bool { return errors.Is(err, context.DeadlineExceeded) }

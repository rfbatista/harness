// Package readshare shares a read among the callers of one unit of work,
// such as a page load: a page whose parts each ask for the same worktree's
// status runs git once. Only reads whose answer cannot change within the
// unit belong here, and their answers are shared, so callers must not
// change them.
package readshare

import (
	"context"
	"sync"
)

type scopeKey struct{}

// scope is one unit of work's reads, by key.
type scope struct {
	mu    sync.Mutex
	reads map[string]any // *run[T]
}

// run is one read under a key. Its answer is set when done is closed, unless
// the run was dropped: then its waiters read again.
type run[T any] struct {
	done    chan struct{}
	v       T
	err     error
	dropped bool
}

// With gives ctx's unit of work its own shared reads.
func With(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &scope{reads: map[string]any{}})
}

// Do answers read's result, run once per key within ctx's scope: callers
// asking at once wait for the one run. A read that fails once its caller's
// ctx has ended is not shared, as its error is that caller's own: those who
// waited for it, and whoever asks next, read again. Without a scope it just
// reads. One key must always name reads of the same type.
func Do[T any](ctx context.Context, key string, read func() (T, error)) (T, error) {
	s, _ := ctx.Value(scopeKey{}).(*scope)
	if s == nil {
		return read()
	}
	for {
		s.mu.Lock()
		r, ok := s.reads[key].(*run[T])
		if !ok {
			r = &run[T]{done: make(chan struct{})}
			s.reads[key] = r
			s.mu.Unlock()
			runRead(ctx, s, key, r, read)
			return r.v, r.err
		}
		s.mu.Unlock()
		<-r.done
		if !r.dropped {
			return r.v, r.err
		}
	}
}

// runRead runs read into r, then lets r's waiters go. A read that fails with
// ctx ended, or panics, is dropped from s first, so it is read again.
func runRead[T any](ctx context.Context, s *scope, key string, r *run[T], read func() (T, error)) {
	finished := false
	defer func() {
		if !finished || (r.err != nil && ctx.Err() != nil) {
			s.mu.Lock()
			if s.reads[key] == any(r) {
				delete(s.reads, key)
			}
			s.mu.Unlock()
			r.dropped = true
		}
		close(r.done)
	}()
	r.v, r.err = read()
	finished = true
}

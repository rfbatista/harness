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
	reads map[string]any // func() (T, error) made by sync.OnceValues
}

// With gives ctx's unit of work its own shared reads.
func With(ctx context.Context) context.Context {
	return context.WithValue(ctx, scopeKey{}, &scope{reads: map[string]any{}})
}

// Do answers read's result, run once per key within ctx's scope: callers
// asking at once wait for the one run. Without a scope it just reads. One
// key must always name reads of the same type.
func Do[T any](ctx context.Context, key string, read func() (T, error)) (T, error) {
	s, _ := ctx.Value(scopeKey{}).(*scope)
	if s == nil {
		return read()
	}
	s.mu.Lock()
	shared, ok := s.reads[key].(func() (T, error))
	if !ok {
		shared = sync.OnceValues(read)
		s.reads[key] = shared
	}
	s.mu.Unlock()
	return shared()
}

package artifacts

import (
	"slices"
	"sync"
)

// keyedLocks serializes work per key: one artifact, one session or one
// task. Work on different keys runs at once. A key's mutex lives only while
// someone holds or waits for it.
//
// Lock order, to stay free of deadlocks: a session's or a task's key before
// any artifact's, and several artifacts' keys in sorted order.
type keyedLocks struct {
	mu   sync.Mutex
	held map[string]*keyedLock
}

type keyedLock struct {
	sync.Mutex
	refs int
}

// lock takes key's mutex and returns its release.
func (k *keyedLocks) lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.held == nil {
		k.held = map[string]*keyedLock{}
	}
	l := k.held[key]
	if l == nil {
		l = &keyedLock{}
		k.held[key] = l
	}
	l.refs++
	k.mu.Unlock()

	l.Lock()
	return func() {
		l.Unlock()
		k.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(k.held, key)
		}
		k.mu.Unlock()
	}
}

// lockAll takes the keys' mutexes in sorted order and returns their release.
func (k *keyedLocks) lockAll(keys []string) (unlock func()) {
	keys = slices.Clone(keys)
	slices.Sort(keys)
	keys = slices.Compact(keys)
	unlocks := make([]func(), len(keys))
	for i, key := range keys {
		unlocks[i] = k.lock(key)
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
}

func artifactKey(id string) string { return "artifact:" + id }
func sessionKey(id string) string  { return "session:" + id }
func ticketKey(id string) string   { return "ticket:" + id }

// lockArtifact serializes everything that writes the artifact's row
// together with its copy, so a row never names a revision whose copy is not
// there, and raises the artifact's changes in the order they were applied.
func (s *Service) lockArtifact(id string) func() { return s.locks.lock(artifactKey(id)) }

// lockSession serializes what creates a session's artifacts with the
// removal of all of them when the session is deleted.
func (s *Service) lockSession(id string) func() { return s.locks.lock(sessionKey(id)) }

// lockTicket serializes attaching to or detaching from a task with the
// detaching of everything when the task is deleted.
func (s *Service) lockTicket(id string) func() { return s.locks.lock(ticketKey(id)) }

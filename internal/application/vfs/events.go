package vfs

import "sync"

// RootEvent says a root of a session's virtual file system went away, or is
// available (again).
type RootEvent struct {
	SessionID string
	Kind      RootKind
	// Path is the root's real path the event is about: the one that went
	// away, or the one now available.
	Path      string
	Available bool
}

// RootEvents is the in-process notification of roots coming and going. Any
// domain subscribes; the virtual file system raises. A subscriber hears
// events in the order they were raised and never holds up the raiser: each
// has its own queue, drained by its own goroutine.
type RootEvents struct {
	mu   sync.Mutex
	subs map[int]*subscriber
	next int
}

// NewRootEvents returns an empty notification.
func NewRootEvents() *RootEvents { return &RootEvents{subs: map[int]*subscriber{}} }

type subscriber struct {
	fn   func(RootEvent)
	mu   sync.Mutex
	cond *sync.Cond
	q    []RootEvent
	done bool
}

// Subscribe has fn called with every event raised from now on, and answers
// the call that stops it. fn runs on its own goroutine, one event at a time.
func (e *RootEvents) Subscribe(fn func(RootEvent)) (cancel func()) {
	sub := &subscriber{fn: fn}
	sub.cond = sync.NewCond(&sub.mu)
	e.mu.Lock()
	id := e.next
	e.next++
	e.subs[id] = sub
	e.mu.Unlock()
	go sub.run()
	return func() {
		e.mu.Lock()
		delete(e.subs, id)
		e.mu.Unlock()
		sub.mu.Lock()
		sub.done = true
		sub.mu.Unlock()
		sub.cond.Signal()
	}
}

func (s *subscriber) run() {
	for {
		s.mu.Lock()
		for len(s.q) == 0 && !s.done {
			s.cond.Wait()
		}
		if s.done {
			s.mu.Unlock()
			return
		}
		ev := s.q[0]
		s.q = s.q[1:]
		s.mu.Unlock()
		s.fn(ev)
	}
}

// raise queues ev for every subscriber. A nil RootEvents raises nothing.
func (e *RootEvents) raise(ev RootEvent) {
	if e == nil {
		return
	}
	e.mu.Lock()
	subs := make([]*subscriber, 0, len(e.subs))
	for _, s := range e.subs {
		subs = append(subs, s)
	}
	e.mu.Unlock()
	for _, s := range subs {
		s.mu.Lock()
		s.q = append(s.q, ev)
		s.mu.Unlock()
		s.cond.Signal()
	}
}

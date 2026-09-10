package backend

import (
	"context"
	"sync"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// SentMessage records one Send call.
type SentMessage struct {
	SessionID string
	Text      string
}

// Decision records one Resolve or Answer call.
type Decision struct {
	SessionID string
	ReqID     string
	Allow     bool
	Message   string
	Answers   map[string]string
	Notes     map[string]string
}

// sessionLog is the fake's per-session event store plus its subscribers.
type sessionLog struct {
	events []orchestration.SessionEvent
	subs   map[int]chan orchestration.SessionEvent
	next   int
}

func (f *Fake) log(id string) *sessionLog {
	if f.logs == nil {
		f.logs = map[string]*sessionLog{}
	}
	l := f.logs[id]
	if l == nil {
		l = &sessionLog{subs: map[int]chan orchestration.SessionEvent{}}
		f.logs[id] = l
	}
	return l
}

// Emit appends a durable event (assigning the next seq) and fans it out.
func (f *Fake) Emit(id string, ev orchestration.SessionEvent) {
	f.mu.Lock()
	l := f.log(id)
	ev.SessionID = id
	ev.Seq = int64(len(l.events) + 1)
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	l.events = append(l.events, ev)
	subs := l.snapshotSubs()
	f.mu.Unlock()
	for _, ch := range subs {
		ch <- ev
	}
}

// EmitDelta fans out an ephemeral event (seq 0) without persisting it.
func (f *Fake) EmitDelta(id string, ev orchestration.SessionEvent) {
	f.mu.Lock()
	l := f.log(id)
	ev.SessionID = id
	ev.Seq = 0
	subs := l.snapshotSubs()
	f.mu.Unlock()
	for _, ch := range subs {
		ch <- ev
	}
}

func (l *sessionLog) snapshotSubs() []chan orchestration.SessionEvent {
	out := make([]chan orchestration.SessionEvent, 0, len(l.subs))
	for _, ch := range l.subs {
		out = append(out, ch)
	}
	return out
}

func (f *Fake) session(id string) *domain.Session {
	for _, s := range f.Sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// --- SessionAccess ---

func (f *Fake) GetSession(id string) *domain.Session { return f.session(id) }

func (f *Fake) Subscribe(id string) (<-chan orchestration.SessionEvent, []orchestration.SessionEvent, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.log(id)
	ch := make(chan orchestration.SessionEvent, 64)
	n := l.next
	l.next++
	l.subs[n] = ch
	replay := append([]orchestration.SessionEvent(nil), l.events...)
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			if c, ok := l.subs[n]; ok {
				delete(l.subs, n)
				close(c)
			}
		})
	}
	return ch, replay, cancel
}

func (f *Fake) History(id string, fromSeq int64) []orchestration.SessionEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []orchestration.SessionEvent
	for _, ev := range f.log(id).events {
		if ev.Seq > fromSeq {
			out = append(out, ev)
		}
	}
	return out
}

func (f *Fake) Send(_ context.Context, id, text string) error {
	if f.Fail != nil {
		return f.Fail
	}
	s := f.session(id)
	if s == nil {
		return notFound("SESSION_NOT_FOUND", "session")
	}
	f.Sent = append(f.Sent, SentMessage{SessionID: id, Text: text})
	s.Status = domain.SessionThinking
	return nil
}

func (f *Fake) Resolve(_ context.Context, id, reqID string, allow bool, message string) error {
	if f.Fail != nil {
		return f.Fail
	}
	if f.session(id) == nil {
		return notFound("SESSION_NOT_FOUND", "session")
	}
	f.Decisions = append(f.Decisions, Decision{SessionID: id, ReqID: reqID, Allow: allow, Message: message})
	return nil
}

func (f *Fake) Answer(_ context.Context, id, reqID string, answers, notes map[string]string) error {
	if f.Fail != nil {
		return f.Fail
	}
	if f.session(id) == nil {
		return notFound("SESSION_NOT_FOUND", "session")
	}
	if len(answers) == 0 {
		return &domain.StructuredError{Code: "NO_ANSWERS", Message: "no answers provided"}
	}
	f.Decisions = append(f.Decisions, Decision{SessionID: id, ReqID: reqID, Allow: true, Answers: answers, Notes: notes})
	return nil
}

func (f *Fake) SetAutoRun(_ context.Context, id string, enabled bool) error {
	if f.Fail != nil {
		return f.Fail
	}
	s := f.session(id)
	if s == nil {
		return notFound("SESSION_NOT_FOUND", "session")
	}
	s.AutoRun = enabled
	return nil
}

func (f *Fake) Stop(_ context.Context, id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	s := f.session(id)
	if s == nil {
		return notFound("SESSION_NOT_FOUND", "session")
	}
	s.Status = domain.SessionStopped
	return nil
}

func (f *Fake) DeleteSession(_ context.Context, id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	for i, s := range f.Sessions {
		if s.ID == id {
			f.Sessions = append(f.Sessions[:i], f.Sessions[i+1:]...)
			return nil
		}
	}
	return notFound("SESSION_NOT_FOUND", "session")
}

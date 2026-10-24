package usage

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The live call log (see "Spec: harness — live call log"): the one in-process
// fan-out of captured calls to the live call viewers. It keeps nothing but
// each viewer's bounded queue, and never makes the producer wait.

var (
	_ ports.CallCapture = (*Service)(nil)
	_ ports.LiveCalls   = (*Service)(nil)
)

const (
	// defaultLiveCallBuffer is how many events a viewer may have waiting
	// before the ones that do not fit are discarded. Calls carry their
	// bodies, so it is kept short.
	defaultLiveCallBuffer = 64
	// sessionTitleLen caps the title shown for a session.
	sessionTitleLen = 80
)

// Enabled reports whether any live call viewer is connected, on any project.
func (s *Service) Enabled() bool {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	return len(s.viewers) > 0
}

// OpenLiveCalls connects a live call viewer for a project.
func (s *Service) OpenLiveCalls(projectID string) ports.LiveCallViewer {
	v := &liveViewer{
		svc: s, project: projectKey(projectID), max: s.liveBuffer,
		started: map[string]struct{}{}, wake: make(chan struct{}, 1),
	}
	s.liveMu.Lock()
	s.viewers[v] = struct{}{}
	s.liveMu.Unlock()
	return v
}

// Started tells the viewers of the call's project, and of no project, that
// the call began.
func (s *Service) Started(ctx context.Context, r ports.CapturedRequest) {
	if !s.Enabled() {
		return
	}
	who := s.resolveCall(ctx, r.Attribution)
	ev := ports.LiveCallEvent{Name: ports.LiveCallStarted, Data: lazily(func() any { return startedEvent(r, who) })}
	for _, v := range s.liveViewers() {
		if who.projectID == "" || who.projectID == v.project {
			v.offerStarted(r.ID, ev)
		}
	}
}

// Finished tells the viewers that received the call's start that it ended.
func (s *Service) Finished(ctx context.Context, c ports.CapturedCall) {
	if !s.Enabled() {
		return
	}
	ev := ports.LiveCallEvent{Name: ports.LiveCallFinished, Data: lazily(func() any { return finishedEvent(c) })}
	for _, v := range s.liveViewers() {
		v.offerFinished(c.ID, ev)
	}
}

// lazyJSON builds an event's JSON on the first marshal, which a viewer's own
// goroutine does, so decoding and parsing a body never run on the proxy's
// path. Viewers share the one result.
type lazyJSON struct {
	once  sync.Once
	build func() any
	out   []byte
	err   error
}

func lazily(build func() any) *lazyJSON { return &lazyJSON{build: build} }

func (l *lazyJSON) MarshalJSON() ([]byte, error) {
	l.once.Do(func() { l.out, l.err = json.Marshal(l.build()) })
	return l.out, l.err
}

func (s *Service) liveViewers() []*liveViewer {
	s.liveMu.Lock()
	defer s.liveMu.Unlock()
	out := make([]*liveViewer, 0, len(s.viewers))
	for v := range s.viewers {
		out = append(out, v)
	}
	return out
}

// callOwner is what a call's attribution resolves to.
type callOwner struct {
	projectID string
	session   *domain.Session
	task      *liveRef
	kind      string
}

func (s *Service) resolveCall(ctx context.Context, attribution string) callOwner {
	o := callOwner{kind: "system"}
	switch {
	case attribution == "" || attribution == ports.UsageSystemAttribution:
	case attribution == documentAssistantAttribution || strings.HasPrefix(attribution, documentAssistantAttribution+":"):
		o.kind = "document-assistant"
		o.projectID = strings.TrimPrefix(strings.TrimPrefix(attribution, documentAssistantAttribution), ":")
	case attribution == writingHelperAttribution || strings.HasPrefix(attribution, writingHelperAttribution+":"):
		o.kind = "writing-helper"
		o.projectID = strings.TrimPrefix(strings.TrimPrefix(attribution, writingHelperAttribution), ":")
	default:
		if sess := s.readers.Sessions.Get(attribution); sess != nil {
			o.session, o.projectID, o.kind = sess, sess.ProjectID, "session"
			if sess.TicketID != "" {
				if t := s.readers.Tickets.Get(ctx, sess.TicketID); t != nil {
					o.task = &liveRef{ID: t.ID, Title: t.Title}
				}
			}
		}
	}
	return o
}

// documentAssistantAttribution is what the document assistant's calls are
// attributed to, with ":<project id>" appended.
const documentAssistantAttribution = "harness:document-assistant"

// writingHelperAttribution is what the writing helper's calls are attributed
// to, with ":<project id>" appended. A stored record keeps the bare name.
const writingHelperAttribution = "harness:writing-helper"

// liveViewer is one live call viewer's queue.
type liveViewer struct {
	svc     *Service
	project string // the stored project id; "" is the harness's own
	max     int
	wake    chan struct{}

	mu       sync.Mutex
	queue    []ports.LiveCallEvent
	missed   int
	missedAt time.Time
	started  map[string]struct{} // the calls whose start the viewer was given
	closed   bool
}

func (v *liveViewer) Ready() <-chan struct{} { return v.wake }

func (v *liveViewer) Close() {
	v.svc.liveMu.Lock()
	delete(v.svc.viewers, v)
	v.svc.liveMu.Unlock()
	v.mu.Lock()
	v.closed = true
	v.queue, v.started = nil, nil
	v.mu.Unlock()
}

func (v *liveViewer) Take() (ports.LiveCallEvent, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case len(v.queue) > 0:
		ev := v.queue[0]
		v.queue = v.queue[1:]
		return ev, true
	case v.missed > 0:
		return v.takeMissed(), true
	}
	return ports.LiveCallEvent{}, false
}

func (v *liveViewer) takeMissed() ports.LiveCallEvent {
	ev := ports.LiveCallEvent{Name: ports.LiveCallsMissed, Data: missedEvent{Count: v.missed, At: stamp(v.missedAt)}}
	v.missed = 0
	return ev
}

func (v *liveViewer) offerStarted(id string, ev ports.LiveCallEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	if v.enqueue(ev) {
		v.started[id] = struct{}{}
	}
	v.signal()
}

func (v *liveViewer) offerFinished(id string, ev ports.LiveCallEvent) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return
	}
	if _, ok := v.started[id]; !ok {
		return
	}
	delete(v.started, id)
	v.enqueue(ev)
	v.signal()
}

// enqueue queues ev, or discards it and counts it missed when the buffer is
// full. Discards are reported, in their place in the order, before whatever
// is queued after them. v.mu is held.
func (v *liveViewer) enqueue(ev ports.LiveCallEvent) bool {
	if v.missed > 0 && len(v.queue) < v.max {
		v.queue = append(v.queue, v.takeMissed())
	}
	if len(v.queue) >= v.max {
		v.missed++
		v.missedAt = v.svc.now()
		return false
	}
	v.queue = append(v.queue, ev)
	return true
}

func (v *liveViewer) signal() {
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

// credentialHeaders are the headers whose values never reach a viewer.
var credentialHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "x-api-key": true, "cookie": true, "set-cookie": true,
}

func redactHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for name, values := range h {
		vs := make([]string, len(values))
		for i, v := range values {
			if credentialHeaders[strings.ToLower(name)] {
				v = ports.RedactedValue
			}
			vs[i] = v
		}
		out[name] = vs
	}
	return out
}

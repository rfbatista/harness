package orchestration

import (
	"context"
	"sync"
	"time"

	"operators-mcp/internal/ports"
)

// contextWindow is how often a session's context change may reach the feed:
// at most once per window, with what came in between coalesced (contract C5).
const contextWindow = 2 * time.Second

// contextNotifier sends one session's context changes to the feed at most
// once per window. The first change after a quiet window goes at once; changes
// inside a window wait and go as one when it ends, so the last is never lost.
type contextNotifier struct {
	mu        sync.Mutex
	now       func() time.Time
	afterFunc func(time.Duration, func())
	send      func(sessionID, reason string)
	sessions  map[string]*notifyState
}

type notifyState struct {
	last    time.Time // when the last message went
	pending string    // the reason waiting for the window's end; "" none
	waiting bool      // a timer is set
}

func newContextNotifier(now func() time.Time, afterFunc func(time.Duration, func()), send func(sessionID, reason string)) *contextNotifier {
	return &contextNotifier{now: now, afterFunc: afterFunc, send: send, sessions: map[string]*notifyState{}}
}

// reasonRank orders the reasons of coalesced changes: the most telling wins.
var reasonRank = map[string]int{ports.ContextUsage: 1, ports.ContextMCPStatus: 2, ports.ContextEntryAdded: 3}

// changed says the session's context changed for reason.
func (n *contextNotifier) changed(sessionID, reason string) {
	n.mu.Lock()
	st := n.sessions[sessionID]
	if st == nil {
		st = &notifyState{}
		n.sessions[sessionID] = st
	}
	now := n.now()
	wait := contextWindow - now.Sub(st.last)
	if !st.last.IsZero() && wait > 0 {
		if reasonRank[reason] >= reasonRank[st.pending] {
			st.pending = reason
		}
		if !st.waiting {
			st.waiting = true
			n.afterFunc(wait, func() { n.flush(sessionID) })
		}
		n.mu.Unlock()
		return
	}
	st.last = now
	n.mu.Unlock()
	n.send(sessionID, reason)
}

// flush sends what a window's end finds waiting.
func (n *contextNotifier) flush(sessionID string) {
	n.mu.Lock()
	st := n.sessions[sessionID]
	reason := st.pending
	st.pending, st.waiting = "", false
	if reason != "" {
		st.last = n.now()
	}
	n.mu.Unlock()
	if reason != "" {
		n.send(sessionID, reason)
	}
}

// contextChanged says a session's context changed, for the project feed.
func (s *Service) contextChanged(sessionID, reason string) {
	if s.contextNotes != nil {
		s.contextNotes.changed(sessionID, reason)
	}
}

// sendContextChange puts a session's context change on its project's feed.
func (s *Service) sendContextChange(sessionID, reason string) {
	sess := s.sessions.Get(sessionID)
	if sess == nil || sess.ProjectID == "" {
		return
	}
	s.feed.send(ports.ProjectChange{ContextChange: &ports.ContextChange{
		Kind: ports.ContextChangeKind, SessionID: sessionID, Reason: reason, ProjectID: sess.ProjectID,
	}})
}

// contextPollEvery is how often the context of the sessions a project's
// followers are watching is brought up to date from their transcripts (C4: use
// may lag the transcript by up to 2 s).
const contextPollEvery = 2 * time.Second

// contextLoop keeps the usage of the watched sessions' context current. A
// terminal session says nothing while a tool runs, so its transcript is read
// on a clock, but only for projects somebody follows and sessions that run.
func (s *Service) contextLoop() {
	t := time.NewTicker(contextPollEvery)
	defer t.Stop()
	for range t.C {
		if s.Contexts == nil || s.TranscriptReader == nil {
			continue
		}
		s.pollContext(context.Background())
	}
}

// pollContext syncs the usage of each running session of each followed project.
func (s *Service) pollContext(ctx context.Context) {
	for _, projectID := range s.feed.followedProjects() {
		for _, sess := range s.sessions.List(ctx, ports.SessionFilter{ProjectID: projectID}) {
			if sess.Status.IsTerminal() {
				continue
			}
			_, _ = s.SyncContextUsage(ctx, sess.ID) // a change tells the feed itself
		}
	}
}

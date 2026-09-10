package session

import "operators-mcp/internal/application/orchestration"

// BackMsg asks the root to return to where the user came from. The
// subscription stays alive so approvals keep arriving.
type BackMsg struct{}

// ClosedMsg tells the root the session was deleted and its screen must go.
type ClosedMsg struct{ ID string }

// Routed is implemented by every message a session screen produces for
// itself, so the root can deliver it to the right screen even when that
// screen is in the background.
type Routed interface{ SessionRef() string }

// EventMsg carries one live event from the subscription.
type EventMsg struct{ Ev orchestration.SessionEvent }

// SessionRef names the session the event belongs to.
func (e EventMsg) SessionRef() string { return e.Ev.SessionID }

// StreamClosedMsg says the subscription channel was closed underneath us.
type StreamClosedMsg struct{ ID string }

// SessionRef names the session whose stream closed.
func (s StreamClosedMsg) SessionRef() string { return s.ID }

// actionKind names a session control, so its outcome lands on the right
// part of the screen: the composer, the decision panel or the header banner.
type actionKind int

const (
	actionSend actionKind = iota + 1
	actionApprove
	actionDeny
	actionAnswer
	actionAutoRun
	actionStop
	actionDelete
)

// actionDoneMsg reports the outcome of one session control.
type actionDoneMsg struct {
	id   string
	kind actionKind
	err  error
}

func (a actionDoneMsg) SessionRef() string { return a.id }

var (
	_ Routed = EventMsg{}
	_ Routed = StreamClosedMsg{}
	_ Routed = actionDoneMsg{}
)

// Package session is the live session screen: the event timeline, the live
// streaming tail, and the intervene bar (composer, approval, questions).
//
// The screen is split by concern: model.go owns the subscription and the
// message loop; fold.go is the pure events-to-timeline fold; feed.go draws
// the timeline; bar.go and the bar_*.go files are the intervene bar, one
// State per mode; question.go collects answers; keys.go holds the controls
// every mode shares.
package session

import (
	"context"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// focus says which half of the screen owns the keyboard.
type focus int

const (
	focusBar focus = iota
	focusFeed
)

// actionTimeout bounds every session control call.
const actionTimeout = 30 * time.Second

// Model is the live session screen.
type Model struct {
	ctx  core.Context
	be   backend.Backend
	snap backend.Snapshot
	id   string
	sess *domain.Session

	events []orchestration.SessionEvent
	tl     Timeline
	deltas Deltas

	ch         <-chan orchestration.SessionEvent
	cancel     func()
	subscribed bool

	vp       viewport.Model
	follow   bool
	cursor   int
	expanded map[int64]bool
	focus    focus

	composer textarea.Model
	sending  bool
	sendErr  string

	deciding    bool
	decideErr   string
	denyMessage *textinput.Model
	question    *questionState

	confirm *components.Confirm
	banner  string
}

// New opens the session: subscribes, seeds the timeline from the replay
// buffer and the durable history, and focuses the composer.
func New(ctx core.Context, be backend.Backend, snap backend.Snapshot, id string) Model {
	m := Model{ctx: ctx, be: be, snap: snap, id: id, follow: true, expanded: map[int64]bool{}}
	m.sess = be.GetSession(id)
	ch, replay, cancel := be.Subscribe(id)
	m.ch, m.cancel, m.subscribed = ch, cancel, true
	// History is the durable log; replay is the recent ring. Union them by seq
	// so a long session is complete and a fresh one has no gap.
	seen := map[int64]bool{}
	for _, ev := range append(be.History(id, 0), replay...) {
		if ev.Seq == 0 || seen[ev.Seq] {
			continue
		}
		seen[ev.Seq] = true
		m.events = append(m.events, ev)
	}
	m.refold()

	ta := textarea.New()
	ta.Placeholder = "Message the agent… (ctrl+s to send)"
	ta.ShowLineNumbers = false
	ta.Focus()
	m.composer = ta

	m.vp = viewport.New(viewport.WithWidth(ctx.Width), viewport.WithHeight(m.feedHeight()))
	m.layout()
	return m
}

// SessionID identifies the screen.
func (m Model) SessionID() string { return m.id }

// Subscribed reports whether the live channel is still open.
func (m Model) Subscribed() bool { return m.subscribed }

// Close cancels the subscription; call it when the screen is discarded.
func (m *Model) Close() {
	if m.cancel != nil && m.subscribed {
		m.cancel()
	}
	m.subscribed = false
}

// Init starts waiting for live events.
func (m Model) Init() tea.Cmd { return m.wait() }

// wait blocks on the next live event and re-arms itself from Update. This is
// the Observer side of orchestration.Service.Subscribe, expressed as a
// recurring tea.Cmd.
func (m Model) wait() tea.Cmd {
	ch := m.ch
	if ch == nil {
		return nil
	}
	id := m.id
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return StreamClosedMsg{ID: id}
		}
		ev.SessionID = id
		return EventMsg{Ev: ev}
	}
}

func (m *Model) refold() {
	m.tl = Fold(m.events)
	if m.question != nil && !m.tl.isPending(m.question.reqID) {
		// The question panel only makes sense while its request is pending.
		m.question = nil
	}
}

// Hints lists the keys for the hint bar.
func (m Model) Hints() []core.KeyHint {
	if m.focus == focusFeed {
		return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "tab", Desc: "to bar"}, {Key: "j/k", Desc: "move"}, {Key: "↵", Desc: "expand"}, {Key: "G", Desc: "follow"}, {Key: "a", Desc: "auto-run"}, {Key: "S", Desc: "stop"}, {Key: "D", Desc: "delete"}}
	}
	return m.bar().hints()
}

// Update handles events, actions and keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		m.ctx = msg.Ctx
		m.layout()
		return m, nil
	case core.SnapshotMsg:
		m.snap = msg.Snapshot
		for _, s := range m.snap.Sessions {
			if s.ID == m.id {
				m.sess = s
			}
		}
		m.layout()
		return m, nil
	case EventMsg:
		if msg.Ev.SessionID != m.id {
			return m, nil
		}
		m.apply(msg.Ev)
		return m, m.wait()
	case StreamClosedMsg:
		m.subscribed = false
		return m, nil
	case actionDoneMsg:
		return m.actionDone(msg)
	case components.ConfirmedMsg:
		m.confirm = nil
		if msg.Tag == confirmDelete {
			return m, m.action(actionDelete, func(ctx context.Context) error { return m.be.DeleteSession(ctx, m.id) })
		}
		return m, nil
	case components.ConfirmCancelledMsg:
		m.confirm = nil
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// apply folds one live event in. Deltas (seq 0) only touch the streaming
// tail; durable events extend the timeline and may move the status.
func (m *Model) apply(ev orchestration.SessionEvent) {
	m.deltas.Apply(ev)
	if ev.Seq != 0 {
		m.events = append(m.events, ev)
		m.refold()
		if ev.Status != "" && m.sess != nil {
			m.sess.Status = ev.Status
		}
	}
	m.layout()
}

// action runs a session control off the UI goroutine and reports back as
// actionDoneMsg.
func (m Model) action(kind actionKind, fn func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
		defer cancel()
		return actionDoneMsg{id: m.id, kind: kind, err: fn(ctx)}
	}
}

// actionDone settles a control: each kind reports into its own part of the
// screen so an error never lands somewhere the user is not looking.
func (m Model) actionDone(msg actionDoneMsg) (Model, tea.Cmd) {
	switch msg.kind {
	case actionSend:
		m.sending = false
		if msg.err != nil {
			m.sendErr = core.Message(msg.err)
			return m, nil
		}
		m.sendErr = ""
		m.composer.Reset()
	case actionApprove, actionDeny, actionAnswer:
		m.deciding = false
		m.denyMessage = nil
		if msg.err != nil {
			m.decideErr = core.Message(msg.err)
			return m, nil
		}
		m.decideErr = ""
		m.question = nil
	case actionAutoRun, actionStop:
		m.banner = ""
		if msg.err != nil {
			m.banner = core.Message(msg.err)
		}
	case actionDelete:
		if msg.err != nil {
			m.banner = core.Message(msg.err)
			break
		}
		m.Close()
		id := m.id
		return m, tea.Batch(
			func() tea.Msg { return ClosedMsg{ID: id} },
			func() tea.Msg { return core.RefreshMsg{} },
		)
	}
	m.layout()
	return m, nil
}

// liveStatus prefers the snapshot's status, which the orchestrator owns, and
// falls back to what the timeline folded.
func (m *Model) liveStatus() domain.SessionStatus {
	if m.sess != nil && m.sess.Status != "" {
		return m.sess.Status
	}
	return m.tl.Status
}

// pendingRequest is the oldest approval still awaiting a decision.
func (m Model) pendingRequest() (Item, bool) {
	if len(m.tl.Pending) == 0 {
		return Item{}, false
	}
	return m.tl.Pending[0], true
}

package orchestration

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TurnDelivery = (*Service)(nil)

// The courier puts turns from other sessions (the architect channel) into a
// running session without ever cutting into a turn in flight. A session that
// is idle takes a turn at once: a headless one through Send, an interactive
// one on the server by typing it into its terminal. Anything else waits in
// the session's outbox and goes in when the current turn ends: on the
// headless "result" line, or through the interactive CLI's Stop hook, whose
// answer makes claude carry on with the waiting turns.

// inputQuiet is how long after a person's last key press the courier keeps
// out of a terminal, so it never types into a half-written prompt.
const inputQuiet = 10 * time.Second

// pasteSettle is the pause between a typed turn and the Enter that submits
// it, so the paste has landed in the prompt first.
const pasteSettle = 300 * time.Millisecond

type parcel struct {
	key, text string
	done      func(time.Time)
}

// turnState is what the server knows of an interactive session's turn.
type turnState int

const (
	turnUnknown turnState = iota // counts as busy: wait for the next Stop
	turnBusy
	turnIdle
)

type courier struct {
	mu        sync.Mutex
	outbox    map[string][]parcel
	turns     map[string]turnState
	lastInput map[string]time.Time
}

// ready makes the maps on first use, so a Service built without NewService
// (tests) has a working courier. Callers hold c.mu.
func (c *courier) ready() {
	if c.outbox == nil {
		c.outbox, c.turns, c.lastInput = map[string][]parcel{}, map[string]turnState{}, map[string]time.Time{}
	}
}

// enqueue adds p to the session's outbox; a parcel with the same key is
// replaced where it stands.
func (c *courier) enqueue(id string, p parcel) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready()
	q := c.outbox[id]
	for i := range q {
		if q[i].key == p.key {
			q[i] = p
			return
		}
	}
	c.outbox[id] = append(q, p)
}

func (c *courier) take(id string) []parcel {
	c.mu.Lock()
	defer c.mu.Unlock()
	q := c.outbox[id]
	delete(c.outbox, id)
	return q
}

func (c *courier) setTurn(id string, t turnState) {
	c.mu.Lock()
	c.ready()
	c.turns[id] = t
	c.mu.Unlock()
}

func (c *courier) turn(id string) turnState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns[id]
}

func (c *courier) touched(id string) {
	c.mu.Lock()
	c.ready()
	c.lastInput[id] = time.Now()
	c.mu.Unlock()
}

func (c *courier) quiet(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Since(c.lastInput[id]) >= inputQuiet
}

func (c *courier) forget(id string) {
	c.mu.Lock()
	delete(c.outbox, id)
	delete(c.turns, id)
	delete(c.lastInput, id)
	c.mu.Unlock()
}

// Deliver puts text into a running session as a user turn: now when it is
// idle, otherwise when its current turn ends (ports.TurnDelivery).
func (s *Service) Deliver(id, key, text string, done func(time.Time)) (bool, error) {
	sess := s.sessions.Get(id)
	if sess == nil || sess.Status.IsTerminal() {
		return false, &domain.StructuredError{Code: "SESSION_NOT_RUNNING", Message: "session is not running"}
	}
	p := parcel{key: key, text: text, done: done}
	if s.deliverNow(sess, p) {
		return true, nil
	}
	s.courier.enqueue(id, p)
	return false, nil
}

// deliverNow hands p to an idle session; false leaves it for the outbox.
func (s *Service) deliverNow(sess *domain.Session, p parcel) bool {
	switch {
	case !sess.Interactive:
		if s.isBusy(sess.ID) {
			return false
		}
		if err := s.Send(context.Background(), sess.ID, p.text); err != nil {
			return false
		}
	case sess.RunsOn == domain.RunnerServer:
		if s.Terminals == nil || s.courier.turn(sess.ID) != turnIdle || !s.courier.quiet(sess.ID) {
			return false
		}
		t, err := s.Terminals.Attach(sess.ID)
		if err != nil || t.Paste(p.text) != nil {
			return false
		}
		s.courier.setTurn(sess.ID, turnBusy)
		time.AfterFunc(pasteSettle, func() { _ = t.Key(ports.KeyEvent{Text: "\r"}) })
		s.publish(sess.ID, SessionEvent{Type: "user_message", Text: p.text, At: time.Now()})
	default:
		return false // a terminal in someone's client: wait for its Stop hook
	}
	if p.done != nil {
		p.done(time.Now())
	}
	return true
}

// flushHeadless sends what waited in a headless session's outbox, as one
// turn, once its turn has ended.
func (s *Service) flushHeadless(id string) {
	q := s.courier.take(id)
	if len(q) == 0 {
		return
	}
	if err := s.Send(context.Background(), id, joinParcels(q)); err != nil {
		slog.Warn("deliver queued turns", "session", id, "err", err)
		return
	}
	now := time.Now()
	for _, p := range q {
		if p.done != nil {
			p.done(now)
		}
	}
}

func joinParcels(q []parcel) string {
	texts := make([]string, len(q))
	for i, p := range q {
		texts[i] = p.text
	}
	return strings.Join(texts, "\n\n---\n\n")
}

// TurnStarted records that an interactive session started a turn (its
// UserPromptSubmit hook).
func (s *Service) TurnStarted(_ context.Context, id string) error {
	if _, err := s.interactiveSession(id); err != nil {
		return err
	}
	s.courier.setTurn(id, turnBusy)
	return nil
}

// TurnEnded is an interactive session's Stop hook. With turns waiting, the
// session goes on with them in the same process, and they are delivered;
// otherwise it is idle, and its turn has ended.
func (s *Service) TurnEnded(ctx context.Context, id string, _ bool) (string, error) {
	if _, err := s.interactiveSession(id); err != nil {
		return "", err
	}
	// stop_hook_active needs no check of its own: the outbox was emptied by
	// the continuation that set it, so it only goes on again for new turns.
	if q := s.courier.take(id); len(q) > 0 {
		text := joinParcels(q)
		now := time.Now()
		s.publish(id, SessionEvent{Type: "user_message", Text: text, At: now})
		for _, p := range q {
			if p.done != nil {
				p.done(now)
			}
		}
		return text, nil
	}
	s.courier.setTurn(id, turnIdle)
	if s.Events != nil {
		if err := s.Events.Publish(ctx, domain.SessionTurnEnded{SessionID: id}); err != nil {
			slog.Warn("announce turn end", "session", id, "err", err)
		}
	}
	return "", nil
}

// inputStamp wraps a terminal a person attached to, so the courier knows
// when they last typed.
type inputStamp struct {
	ports.Terminal
	touched func()
}

func (t inputStamp) Key(k ports.KeyEvent) error {
	t.touched()
	return t.Terminal.Key(k)
}

func (t inputStamp) Paste(text string) error {
	t.touched()
	return t.Terminal.Paste(text)
}

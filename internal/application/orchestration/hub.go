package orchestration

import "sync"

// Hub fans SessionEvents out to per-session subscribers and keeps a bounded
// replay ring for late joiners. A subscriber that cannot take an event is
// closed rather than kept with a gap (ADR 0012): it resumes from the durable
// log by seq. Publishing never blocks.
type Hub struct {
	mu         sync.Mutex
	subs       map[string]map[int]chan SessionEvent
	replay     map[string][]SessionEvent
	nextSub    int
	replaySize int
}

func NewHub(replaySize int) *Hub {
	if replaySize <= 0 {
		replaySize = 256
	}
	return &Hub{
		subs:       map[string]map[int]chan SessionEvent{},
		replay:     map[string][]SessionEvent{},
		replaySize: replaySize,
	}
}

func (h *Hub) Publish(sessionID string, ev SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	buf := append(h.replay[sessionID], ev)
	if len(buf) > h.replaySize {
		buf = buf[len(buf)-h.replaySize:]
	}
	h.replay[sessionID] = buf
	h.fanOut(sessionID, ev)
}

// PublishEphemeral fans an event out to live subscribers only. Unlike Publish
// it does NOT append to the replay ring, so late joiners never see it. Used for
// streaming token deltas, which are reconciled by the persisted consolidated
// output event.
func (h *Hub) PublishEphemeral(sessionID string, ev SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.fanOut(sessionID, ev)
}

// fanOut hands ev to each subscriber of the session; one whose buffer is full
// is removed and closed. Callers hold h.mu.
func (h *Hub) fanOut(sessionID string, ev SessionEvent) {
	subs := h.subs[sessionID]
	for id, ch := range subs {
		select {
		case ch <- ev:
		default:
			delete(subs, id)
			close(ch)
		}
	}
	if len(subs) == 0 {
		delete(h.subs, sessionID)
	}
}

// End forgets an ended session: its subscribers are closed after the events
// already buffered for them, and its replay ring is dropped. Its events stay
// in the durable log for anyone who follows it again.
func (h *Hub) End(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[sessionID] {
		close(ch)
	}
	delete(h.subs, sessionID)
	delete(h.replay, sessionID)
}

// subscriberBuffer is how many events a subscriber may have waiting before it
// is closed as a slow subscriber. A burst of streamed output deltas comes
// faster than any other event, so it is sized for one to pass while the
// follower's goroutine waits to be scheduled.
const subscriberBuffer = 256

func (h *Hub) Subscribe(sessionID string) (<-chan SessionEvent, []SessionEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan SessionEvent, subscriberBuffer)
	id := h.nextSub
	h.nextSub++
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = map[int]chan SessionEvent{}
	}
	h.subs[sessionID][id] = ch

	replay := append([]SessionEvent(nil), h.replay[sessionID]...)

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if m := h.subs[sessionID]; m != nil {
			if c, ok := m[id]; ok {
				delete(m, id)
				close(c)
			}
			if len(m) == 0 {
				delete(h.subs, sessionID)
			}
		}
	}
	return ch, replay, cancel
}

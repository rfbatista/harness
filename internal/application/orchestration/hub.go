package orchestration

import "sync"

// Hub fans SessionEvents out to per-session subscribers and keeps a bounded
// replay ring for late joiners.
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

	for _, ch := range h.subs[sessionID] {
		select {
		case ch <- ev:
		default: // slow subscriber: drop (durable log + replay cover it)
		}
	}
}

// PublishEphemeral fans an event out to live subscribers only. Unlike Publish
// it does NOT append to the replay ring, so late joiners never see it. Used for
// streaming token deltas, which are reconciled by the persisted consolidated
// output event.
func (h *Hub) PublishEphemeral(sessionID string, ev SessionEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs[sessionID] {
		select {
		case ch <- ev:
		default: // slow subscriber: drop (consolidated output event covers it)
		}
	}
}

func (h *Hub) Subscribe(sessionID string) (<-chan SessionEvent, []SessionEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan SessionEvent, 64)
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
		}
	}
	return ch, replay, cancel
}

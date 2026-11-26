package orchestration

import (
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The vigia de memória's events (Contract: harness server ↔ harness web —
// memória da sessão): a sample every second on the session's stream, live only;
// a sample on the project feed at most every 5 s or on a 5 % move; every
// encerramento por memória on both, durable and never throttled; and the last
// of each kept for the initial read.

const (
	// feedMemoryEvery is the longest a session's sample stays off the project feed.
	feedMemoryEvery = 5 * time.Second
	// feedMemoryMove is the move of total RSS, as a fraction, that publishes
	// sooner.
	feedMemoryMove = 0.05
)

// memoryState is each session's last sample, its kills and what the feed last
// carried.
type memoryState struct {
	mu   sync.Mutex
	last map[string]*domain.MemorySample
	// kills of the session's run.
	kills map[string][]domain.MemoryKill
	fed   map[string]fedSample
}

type fedSample struct {
	at  time.Time
	rss int64
}

// Sampled publishes one sample of a session's tree.
func (s *Service) Sampled(m domain.MemorySample) {
	sess := s.sessions.Get(m.SessionID)
	if sess == nil {
		return
	}
	if m.TaskID == "" {
		m.TaskID = sess.TicketID
	}
	// The last, empty sample clears what a consumer shows; afterwards the
	// session reads as having none.
	empty := m.Processes == 0 && m.RSSBytes == 0
	s.memory.mu.Lock()
	if s.memory.last == nil {
		s.memory.last, s.memory.fed = map[string]*domain.MemorySample{}, map[string]fedSample{}
	}
	if empty {
		delete(s.memory.last, m.SessionID)
	} else {
		sample := m
		s.memory.last[m.SessionID] = &sample
	}
	fed, seen := s.memory.fed[m.SessionID]
	publish := empty || !seen || m.SampledAt.Sub(fed.at) >= feedMemoryEvery || moved(fed.rss, m.RSSBytes)
	if publish {
		s.memory.fed[m.SessionID] = fedSample{at: m.SampledAt, rss: m.RSSBytes}
	}
	if empty {
		delete(s.memory.fed, m.SessionID)
	}
	s.memory.mu.Unlock()

	ev := SessionEvent{Type: ports.MemorySampleEventType, SessionID: m.SessionID, At: m.SampledAt, MemorySample: &m}
	s.hub.PublishEphemeral(m.SessionID, ev)
	if publish && sess.Mode != domain.SessionModeProductOwner {
		s.feed.send(ports.ProjectChange{Memory: &ports.MemoryChange{ProjectID: sess.ProjectID, MemorySample: &m}})
	}
}

func moved(from, to int64) bool {
	if from == 0 {
		return to != 0
	}
	d := float64(to-from) / float64(from)
	return d > feedMemoryMove || d < -feedMemoryMove
}

// publishMemoryKill puts a kill on the session's log, stream and feed, and
// keeps it for the initial read.
func (s *Service) publishMemoryKill(k domain.MemoryKill) {
	sess := s.sessions.Get(k.SessionID)
	if sess == nil {
		return
	}
	if k.TaskID == "" {
		k.TaskID = sess.TicketID
	}
	s.memory.mu.Lock()
	if s.memory.kills == nil {
		s.memory.kills = map[string][]domain.MemoryKill{}
	}
	s.memory.kills[k.SessionID] = append(s.memory.kills[k.SessionID], k)
	s.memory.mu.Unlock()
	s.record(k.SessionID, SessionEvent{Type: ports.MemoryKilledEventType, SessionID: k.SessionID, At: k.KilledAt, MemoryKilled: &k}, sessionChange{})
	s.feed.send(ports.ProjectChange{MemoryKilled: &ports.MemoryKilledChange{ProjectID: sess.ProjectID, MemoryKill: &k}})
}

// lastMemory is the session's last sample while it is live, else nil.
func (s *Service) lastMemory(sess *domain.Session) *domain.MemorySample {
	if sess.Status.IsTerminal() {
		return nil
	}
	s.memory.mu.Lock()
	defer s.memory.mu.Unlock()
	if m := s.memory.last[sess.ID]; m != nil {
		c := *m
		return &c
	}
	return nil
}

// SessionMemory is the initial read of a live session: its last sample (empty,
// with a null sampled_at, before the first), the kills of its run, and where its
// orçamento and prevenção came from; live is false when it is not running.
func (s *Service) SessionMemory(id string) (domain.MemoryRead, bool) {
	sess := s.sessions.Get(id)
	if sess == nil || sess.Status.IsTerminal() {
		return domain.MemoryRead{}, false
	}
	read := domain.MemoryRead{
		Kind: domain.MemorySampleKind, SessionID: id, TaskID: sess.TicketID, Budget: sess.MemoryBudgetBytes,
		BudgetSource: sess.MemoryBudgetSource, Prevention: domain.MemoryPrevention{HeapMB: sess.MemoryHeapMB, TestWorkers: sess.MemoryTestWorkers},
	}
	if m := s.lastMemory(sess); m != nil {
		at := m.SampledAt
		read.SampledAt, read.RSSBytes, read.Budget, read.Processes, read.Heaviest = &at, m.RSSBytes, m.Budget, m.Processes, m.Heaviest
	}
	s.memory.mu.Lock()
	read.Kills = append([]domain.MemoryKill{}, s.memory.kills[id]...)
	s.memory.mu.Unlock()
	return read, true
}

// forgetMemory drops what a session's run left, when it starts again.
func (s *Service) forgetMemory(id string) {
	s.memory.mu.Lock()
	delete(s.memory.kills, id)
	s.memory.mu.Unlock()
}

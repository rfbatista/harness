package domain

import "time"

// MemorySample is one sample of a session's árvore de processos (the
// contract's session.memory event).
type MemorySample struct {
	Kind      string    `json:"kind"`
	SessionID string    `json:"session_id"`
	TaskID    string    `json:"task_id"`
	SampledAt time.Time `json:"sampled_at"`
	RSSBytes  int64     `json:"rss_bytes"`
	Budget    int64     `json:"budget_bytes"`
	Processes int       `json:"processes"`
	// Heaviest is the heaviest eligible process; nil when there is none.
	Heaviest *MemoryProcess `json:"heaviest"`
}

// MemoryProcess names one process of a session's tree: its pid, a command
// that is the process name plus at most its first argument (80 characters at
// most, never the full command line), and its RSS.
type MemoryProcess struct {
	PID      int    `json:"pid"`
	Command  string `json:"command"`
	RSSBytes int64  `json:"rss_bytes"`
}

// MemoryKill is one encerramento por memória (the contract's
// session.memory_killed event).
type MemoryKill struct {
	Kind      string    `json:"kind"`
	SessionID string    `json:"session_id"`
	TaskID    string    `json:"task_id"`
	KilledAt  time.Time `json:"killed_at"`
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	RSSBytes  int64     `json:"rss_bytes"`
	TreeRSS   int64     `json:"tree_rss_bytes"`
	Budget    int64     `json:"budget_bytes"`
	// Signal is the signal that ended the process: SIGTERM, or SIGKILL when it
	// outlived the grace.
	Signal string `json:"signal"`
}

// The kinds of the two events (Contract: harness server ↔ harness web — memória
// da sessão).
const (
	MemorySampleKind = "session.memory"
	MemoryKilledKind = "session.memory_killed"
)

// MemoryCommandMax is the longest command an event carries.
const MemoryCommandMax = 80

// MemoryRead is GET /api/sessions/:id/memory: the last sample of a live
// session in the event's shape (sampled_at null before the first), the kills
// of its run, where its orçamento came from and the prevenção injected at start.
type MemoryRead struct {
	Kind         string           `json:"kind"`
	SessionID    string           `json:"session_id"`
	TaskID       string           `json:"task_id"`
	SampledAt    *time.Time       `json:"sampled_at"`
	RSSBytes     int64            `json:"rss_bytes"`
	Budget       int64            `json:"budget_bytes"`
	Processes    int              `json:"processes"`
	Heaviest     *MemoryProcess   `json:"heaviest"`
	Kills        []MemoryKill     `json:"kills"`
	BudgetSource string           `json:"budget_source"`
	Prevention   MemoryPrevention `json:"prevention"`
}

// MemoryPrevention is what prevenção de memória injected: the Node heap ceiling
// in MB and the test worker count.
type MemoryPrevention struct {
	HeapMB      int64 `json:"heap_mb"`
	TestWorkers int64 `json:"test_workers"`
}

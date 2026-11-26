package ports

import (
	"context"
	"syscall"

	"operators-mcp/internal/domain"
)

// ProcessInfo is one row of the process table, as the vigia de memória reads
// it. It carries no full command line, which can hold secrets: Name and the
// first argument are all that ever reaches an event.
type ProcessInfo struct {
	PID, PPID int
	// SID is the POSIX session id.
	SID       int
	RSSBytes  int64
	Name, Arg string
	// Started is the process's start time as the table prints it: with the pid
	// it says whether a pid is still the process it was.
	Started string
	// Session is the agent conversation id a process was started for
	// (claude's --session-id or --resume value); "" for every other process.
	// It is how the agent CLI is found in the table.
	Session string
}

// ProcessTable reads the machine's process table: every process, once.
type ProcessTable interface {
	Read(ctx context.Context) ([]ProcessInfo, error)
}

// ProcessSignaler sends a signal to a process. A process that is already gone
// is not an error.
type ProcessSignaler interface {
	Signal(pid int, sig syscall.Signal) error
}

// MemoryWatch is one session handed to the vigia de memória.
type MemoryWatch struct {
	SessionID, TaskID string
	// BudgetBytes is the session's orçamento de memória, fixed for this watch.
	BudgetBytes int64
	// ClaudeID is the conversation id the agent CLI was started with; the CLI
	// is the process whose ProcessInfo.Session equals it.
	ClaudeID string
	// SID is the POSIX session id of the session's PTY; 0 for a headless
	// session, which shares the server's and is watched by descendancy only.
	SID int
	// RootPID is the PTY's process, which leads that POSIX session: with SID
	// 0 the vigia takes the session id from its table row.
	RootPID int
}

// MemoryWatcher watches sessions' process trees. Watch starts watching and
// answers what stops it (and publishes the last, empty sample).
type MemoryWatcher interface {
	Watch(MemoryWatch) (stop func())
}

// MemoryNotices tells the agent of an encerramento por memória in the answer
// to a Bash call: BashStarted when the call begins, BashEnded when it ends,
// answering the reason ("" when nothing was ended meanwhile).
type MemoryNotices interface {
	BashStarted(sessionID, callID string)
	BashEnded(sessionID, callID string) string
}

// MemoryReader is the initial read of a live session's memory; live is false when the session is not running.
type MemoryReader interface {
	SessionMemory(sessionID string) (read domain.MemoryRead, live bool)
}

// MemoryEnding is an optional part of a sink: it hears of an encerramento por
// memória the moment SIGTERM is sent, before the process is seen gone, so the
// agent can be told in the answer of the call that was running (ADR 0006).
type MemoryEnding interface {
	Ending(domain.MemoryKill)
}

// MemorySink receives what the vigia de memória sees: a sample of each
// watched session every second, and each encerramento por memória.
type MemorySink interface {
	Sampled(domain.MemorySample)
	Killed(domain.MemoryKill)
}

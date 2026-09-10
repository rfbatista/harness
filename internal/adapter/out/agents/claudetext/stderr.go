package claudetext

import (
	"io"
	"strings"
	"sync"
)

// stderrTailLimit is how much of a session's stderr is kept for the exit
// report. The CLI's fatal messages are short; the tail only has to explain why
// the process died.
const stderrTailLimit = 2048

// stderrTail keeps the last stderrTailLimit bytes written to it while teeing
// everything through to tee (the server's own stderr), so a crashed session can
// say *why* it crashed instead of surfacing a bare "failed".
//
// Written by the process's stderr pump goroutine and read by readLoop after
// Wait, hence the mutex.
type stderrTail struct {
	tee io.Writer

	mu  sync.Mutex
	buf []byte
}

func newStderrTail(tee io.Writer) *stderrTail {
	return &stderrTail{tee: tee}
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > stderrTailLimit {
		t.buf = t.buf[len(t.buf)-stderrTailLimit:]
	}
	t.mu.Unlock()

	if t.tee != nil {
		// A failing terminal must not fail the session: the tail is still kept.
		_, _ = t.tee.Write(p)
	}
	return len(p), nil
}

// String returns the retained tail, trimmed. Empty when the process wrote
// nothing to stderr.
func (t *stderrTail) String() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

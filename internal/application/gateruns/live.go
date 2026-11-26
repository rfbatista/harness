package gateruns

import (
	"context"
	"io"
	"sync"

	"operators-mcp/internal/domain"
)

// maxLiveBytes is what a running gate keeps for readers that join late; a gate
// that prints more keeps printing, but a late reader gets only this much.
const maxLiveBytes = 4 << 20

// liveGate is the output of one gate while its run is going: readers get what
// was written, then the rest, until the gate ends.
type liveGate struct {
	mu   sync.Mutex
	buf  []byte
	done bool
	wake chan struct{} // closed and replaced on every change
}

func newLiveGate() *liveGate { return &liveGate{wake: make(chan struct{})} }

func (l *liveGate) Write(p []byte) (int, error) {
	l.mu.Lock()
	if room := maxLiveBytes - len(l.buf); room > 0 {
		l.buf = append(l.buf, p[:min(room, len(p))]...)
	}
	l.broadcast()
	l.mu.Unlock()
	return len(p), nil
}

func (l *liveGate) finish() {
	l.mu.Lock()
	l.done = true
	l.broadcast()
	l.mu.Unlock()
}

func (l *liveGate) broadcast() {
	close(l.wake)
	l.wake = make(chan struct{})
}

// stream writes the output to w as it comes, and returns when the gate has
// ended and everything is written, or when ctx ends.
func (l *liveGate) stream(ctx context.Context, w io.Writer) error {
	pos := 0
	for {
		l.mu.Lock()
		chunk := append([]byte(nil), l.buf[pos:]...)
		done, wake := l.done, l.wake
		l.mu.Unlock()
		if len(chunk) > 0 {
			if _, err := w.Write(chunk); err != nil {
				return err
			}
			pos += len(chunk)
			continue
		}
		if done {
			return nil
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// liveRuns are the gates of the runs in progress, by run id then gate kind.
type liveRuns struct {
	mu    sync.Mutex
	gates map[string]map[domain.GateKind]*liveGate
}

func (r *liveRuns) start(runID string, kinds []domain.GateKind) map[domain.GateKind]*liveGate {
	m := map[domain.GateKind]*liveGate{}
	for _, k := range kinds {
		m[k] = newLiveGate()
	}
	r.mu.Lock()
	if r.gates == nil {
		r.gates = map[string]map[domain.GateKind]*liveGate{}
	}
	r.gates[runID] = m
	r.mu.Unlock()
	return m
}

func (r *liveRuns) end(runID string) {
	r.mu.Lock()
	delete(r.gates, runID)
	r.mu.Unlock()
}

func (r *liveRuns) find(runID string, kind domain.GateKind) (*liveGate, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.gates[runID][kind]
	return g, ok
}

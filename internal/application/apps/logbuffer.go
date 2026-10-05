package apps

import (
	"strings"
	"sync"

	"operators-mcp/internal/ports"
)

// maxLog is how much of a run's output is kept to replay: the tail.
const maxLog = 2 << 20

// logBuffer keeps the tail of a run's output and fans the live output out to
// its viewers. Taking the replay and joining the live stream happen under one
// lock, so a viewer neither misses nor repeats a byte.
type logBuffer struct {
	mu   sync.Mutex
	buf  []byte
	subs map[chan []byte]struct{}
	done bool
}

func newLogBuffer() *logBuffer { return &logBuffer{subs: map[chan []byte]struct{}{}} }

// collect follows the terminal until its process is gone. A subscription the
// host dropped (it fell behind) is renewed; what was printed in between is
// not in the log.
func (l *logBuffer) collect(term ports.Terminal) {
	snap, sub := term.Subscribe()
	// What it printed before this subscription is only on the screen.
	if screen := strings.TrimRight(snap.Screen, "\n "); screen != "" {
		l.append([]byte(strings.ReplaceAll(screen, "\n", "\r\n") + "\r\n"))
	}
	for {
		b, ok := <-sub.C
		if ok {
			l.append(b)
			continue
		}
		select {
		case <-term.Done():
			l.finish()
			return
		default:
			_, sub = term.Subscribe()
		}
	}
}

func (l *logBuffer) append(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, b...)
	if over := len(l.buf) - maxLog; over > 0 {
		l.buf = append([]byte(nil), l.buf[over:]...)
	}
	for ch := range l.subs {
		select {
		case ch <- b:
		default: // too slow: drop it; the viewer resyncs and replays
			delete(l.subs, ch)
			close(ch)
		}
	}
}

func (l *logBuffer) finish() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.done = true
	for ch := range l.subs {
		close(ch)
	}
	l.subs = map[chan []byte]struct{}{}
}

// join returns the output so far and a channel of what follows; the channel
// is closed when the process is gone.
func (l *logBuffer) join() ([]byte, chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	replay := append([]byte(nil), l.buf...)
	ch := make(chan []byte, 1024)
	if l.done {
		close(ch)
	} else {
		l.subs[ch] = struct{}{}
	}
	return replay, ch
}

func (l *logBuffer) leave(ch chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.subs[ch]; ok {
		delete(l.subs, ch)
		close(ch)
	}
}

// logTerminal is a run's terminal whose Subscribe replays the run's output
// so far as the snapshot: a client draws the whole log, not one screen.
type logTerminal struct {
	ports.Terminal
	log *logBuffer
}

func (t *logTerminal) Subscribe() (ports.TerminalSnapshot, ports.Subscription) {
	inner, innerSub := t.Terminal.Subscribe()
	innerSub.Close() // only its size, cursor and title are wanted
	replay, ch := t.log.join()
	snap := ports.TerminalSnapshot{
		Screen:  string(replay),
		CursorX: inner.CursorX,
		CursorY: inner.CursorY,
		Size:    inner.Size,
		Title:   inner.Title,
		Log:     true,
	}
	return snap, ports.Subscription{C: ch, Close: func() { t.log.leave(ch) }}
}

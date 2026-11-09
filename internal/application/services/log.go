package services

import (
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"operators-mcp/internal/ports"
)

const (
	// DefaultLogCap is how much of a run's output the log keeps: the last 5 MB.
	DefaultLogCap = 5 << 20
	// logFollowerBuffer is how many events a log follower may lag behind: a
	// catch-up of a whole log is at most 20 chunks.
	logFollowerBuffer = 256
	// pumpBuffer is how much of a process's output is read at once.
	pumpBuffer = 32 << 10
)

// logFile is one service's log: a file of the last bytes of the run's
// output, whose first byte sits at offset first since the run began. Offsets
// only grow. The file may run a little past the cap before it is cut back,
// so the cut is not made on every write; reads and offsets show exactly the
// last cap bytes.
type logFile struct {
	path string
	cap  int64

	mu    sync.Mutex
	runID string
	first int64 // offset of the file's first byte
	size  int64 // bytes in the file
	subs  map[chan ports.ServiceLogEvent]struct{}
}

// sidecar keeps first across a new instance on the same data directory.
func (l *logFile) sidecar() string { return l.path + ".first" }

// openLog reads what the log file holds now. runID is the service's last
// run, which the file belongs to.
func openLog(path string, logCap int64, runID string) *logFile {
	l := &logFile{path: path, cap: logCap, runID: runID, subs: map[chan ports.ServiceLogEvent]struct{}{}}
	if fi, err := os.Stat(path); err == nil {
		l.size = fi.Size()
	}
	if b, err := os.ReadFile(l.sidecar()); err == nil {
		l.first, _ = strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	}
	return l
}

// end is the offset after the last byte. The caller holds mu.
func (l *logFile) end() int64 { return l.first + l.size }

// firstOffset is the first offset a read can start at. The caller holds mu.
func (l *logFile) firstOffset() int64 { return max(l.first, l.end()-l.cap) }

// reset clears the log for a new run and tells the followers.
func (l *logFile) reset(runID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runID, l.first, l.size = runID, 0, 0
	_ = os.Remove(l.sidecar())
	if err := os.WriteFile(l.path, nil, 0o600); err != nil {
		return err
	}
	l.fan(ports.ServiceLogEvent{Kind: ports.ServiceLogReset, RunID: runID})
	return nil
}

// append adds b to the end of the log and tells the followers.
func (l *logFile) append(b []byte) {
	if len(b) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	n, _ := f.Write(b)
	_ = f.Close()
	at := l.end()
	l.size += int64(n)
	l.fan(ports.ServiceLogEvent{Kind: ports.ServiceLogChunk, RunID: l.runID, Offset: &at, Text: asText(b[:n])})
	if l.size > l.cap+l.cap/8 {
		l.cutLocked()
	}
}

// cutLocked drops the oldest bytes beyond the cap from the file.
func (l *logFile) cutLocked() {
	f, err := os.Open(l.path)
	if err != nil {
		return
	}
	defer f.Close()
	drop := l.size - l.cap
	keep := make([]byte, l.cap)
	if _, err := f.ReadAt(keep, drop); err != nil && err != io.EOF {
		return
	}
	tmp := l.path + ".tmp"
	if os.WriteFile(tmp, keep, 0o600) != nil {
		return
	}
	// The sidecar first: a crash between the two leaves a file that reads as
	// starting later than it does, never earlier.
	if os.WriteFile(l.sidecar(), []byte(strconv.FormatInt(l.first+drop, 10)), 0o600) != nil {
		_ = os.Remove(tmp)
		return
	}
	if os.Rename(tmp, l.path) != nil {
		return
	}
	l.first += drop
	l.size = l.cap
}

// fan sends e to every follower; one that is a whole buffer behind is closed.
// The caller holds mu.
func (l *logFile) fan(e ports.ServiceLogEvent) {
	for ch := range l.subs {
		select {
		case ch <- e:
		default:
			delete(l.subs, ch)
			close(ch)
		}
	}
}

// read returns the bytes from offset (nil: the last tail bytes) as text, at
// most limit of them, and where they start. A read ends on a character
// boundary unless that is the end of the log.
func (l *logFile) read(offset *int64, tail, limit int64) (text string, start, next, first int64, runID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	first, runID = l.firstOffset(), l.runID
	end := l.end()
	switch {
	case offset == nil:
		start = end - tail
	default:
		start = *offset
	}
	start = min(max(start, first), end)
	b := l.bytesLocked(start, min(end-start, limit))
	if int64(len(b)) == limit && start+int64(len(b)) < end {
		b = trimPartialRune(b)
	}
	return asText(b), start, start + int64(len(b)), first, runID
}

// bytesLocked reads n bytes at offset from the file. The caller holds mu.
func (l *logFile) bytesLocked(offset, n int64) []byte {
	if n <= 0 {
		return nil
	}
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	got, _ := f.ReadAt(b, offset-l.first)
	return b[:got]
}

// asText is b as UTF-8: each byte that is not part of a character becomes one
// replacement character.
func asText(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return string([]rune(string(b)))
}

// trimPartialRune cuts a character the end of b begins but does not finish.
func trimPartialRune(b []byte) []byte {
	for i := 1; i <= utf8.UTFMax-1 && i <= len(b); i++ {
		c := b[len(b)-i]
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			break
		}
	}
	return b
}

// follow registers a follower from offset from (nil: the end) until it is
// dropped; what the log holds from there comes first, in chunks.
func (l *logFile) follow(from *int64) <-chan ports.ServiceLogEvent {
	ch := make(chan ports.ServiceLogEvent, logFollowerBuffer)
	l.mu.Lock()
	defer l.mu.Unlock()
	end := l.end()
	at := end
	if from != nil {
		at = min(max(*from, l.firstOffset()), end)
	}
	for at < end {
		b := l.bytesLocked(at, min(end-at, ports.ServiceLogMaxRead))
		if len(b) == 0 {
			break
		}
		if int64(len(b)) == ports.ServiceLogMaxRead && at+int64(len(b)) < end {
			b = trimPartialRune(b)
		}
		off := at
		ch <- ports.ServiceLogEvent{Kind: ports.ServiceLogChunk, RunID: l.runID, Offset: &off, Text: asText(b)}
		at += int64(len(b))
	}
	l.subs[ch] = struct{}{}
	return ch
}

// unfollow closes ch unless fan already did.
func (l *logFile) unfollow(ch <-chan ports.ServiceLogEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for c := range l.subs {
		if (<-chan ports.ServiceLogEvent)(c) == ch {
			delete(l.subs, c)
			close(c)
		}
	}
}

// pump copies a process's output into the log until EOF. A character cut by
// a read is held back until its rest arrives, so chunks are whole text.
func (l *logFile) pump(r io.Reader) {
	buf := make([]byte, pumpBuffer)
	var carry []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := append(carry, buf[:n]...)
			whole := trimPartialRune(chunk)
			l.append(whole)
			carry = append([]byte(nil), chunk[len(whole):]...)
		}
		if err != nil {
			l.append(carry)
			return
		}
	}
}

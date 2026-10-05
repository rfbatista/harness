package apps

import (
	"operators-mcp/internal/ports"
)

// endedTerminal stands in for the terminal of a run that exited before it
// could be attached: nothing to type into, its exit already recorded.
type endedTerminal struct {
	run *run
	svc *Service
}

var closed = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

func (t *endedTerminal) Subscribe() (ports.TerminalSnapshot, ports.Subscription) {
	c := make(chan []byte)
	close(c)
	return ports.TerminalSnapshot{Size: ports.TermSize{Cols: startCols, Rows: startRows}}, ports.Subscription{C: c, Close: func() {}}
}
func (t *endedTerminal) Key(ports.KeyEvent) error    { return nil }
func (t *endedTerminal) Paste(string) error          { return nil }
func (t *endedTerminal) Resize(ports.TermSize) error { return nil }
func (t *endedTerminal) Title() string               { return "" }
func (t *endedTerminal) Kill() error                 { return nil }
func (t *endedTerminal) Done() <-chan struct{}       { return closed }
func (t *endedTerminal) ExitCode() int {
	t.svc.mu.Lock()
	defer t.svc.mu.Unlock()
	return t.run.rec.ExitCode
}

var _ ports.Terminal = (*endedTerminal)(nil)

package portstest

import (
	"sync"

	"operators-mcp/internal/ports"
)

// SandboxCall is one Wrap a RecordingSandboxDriver was asked for.
type SandboxCall struct {
	Policy  ports.SandboxPolicy
	Command ports.SandboxCommand
}

// RecordingSandboxDriver is a ports.SandboxDriver that confines nothing: it
// records the policy and command of each Wrap and answers the command as it
// is.
type RecordingSandboxDriver struct {
	mu    sync.Mutex
	calls []SandboxCall
	// Err, if set, is what Wrap answers.
	Err error
}

var _ ports.SandboxDriver = (*RecordingSandboxDriver)(nil)

func (d *RecordingSandboxDriver) Name() string { return "recording" }

func (d *RecordingSandboxDriver) Wrap(p ports.SandboxPolicy, c ports.SandboxCommand) (ports.SandboxCommand, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, SandboxCall{Policy: p, Command: c})
	if d.Err != nil {
		return ports.SandboxCommand{}, d.Err
	}
	return c, nil
}

// Calls are the Wraps so far, oldest first.
func (d *RecordingSandboxDriver) Calls() []SandboxCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]SandboxCall(nil), d.calls...)
}

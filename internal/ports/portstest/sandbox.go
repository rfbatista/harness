package portstest

import (
	"context"
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

// FakeViolationSource is a ports.SandboxViolationSource the test drives: each
// Follow is recorded, and Emit sends a violation to those following a working
// directory.
type FakeViolationSource struct {
	mu      sync.Mutex
	follows []*fakeFollow
	// Err, if set, is what Follow answers.
	Err error
}

type fakeFollow struct {
	dir string
	ctx context.Context
	ch  chan ports.SandboxViolation
}

var _ ports.SandboxViolationSource = (*FakeViolationSource)(nil)

func (f *FakeViolationSource) Follow(ctx context.Context, workingDir string) (<-chan ports.SandboxViolation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	fl := &fakeFollow{dir: workingDir, ctx: ctx, ch: make(chan ports.SandboxViolation)}
	f.mu.Lock()
	f.follows = append(f.follows, fl)
	f.mu.Unlock()
	go func() {
		<-ctx.Done()
		close(fl.ch)
	}()
	return fl.ch, nil
}

// Following says whether a Follow of workingDir is still running.
func (f *FakeViolationSource) Following(workingDir string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fl := range f.follows {
		if fl.dir == workingDir && fl.ctx.Err() == nil {
			return true
		}
	}
	return false
}

// Follows counts the Follow calls so far.
func (f *FakeViolationSource) Follows() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.follows)
}

// Emit delivers v to every running Follow of workingDir, returning once they
// have taken it.
func (f *FakeViolationSource) Emit(workingDir string, v ports.SandboxViolation) {
	f.mu.Lock()
	fs := append([]*fakeFollow(nil), f.follows...)
	f.mu.Unlock()
	for _, fl := range fs {
		if fl.dir != workingDir || fl.ctx.Err() != nil {
			continue
		}
		select {
		case fl.ch <- v:
		case <-fl.ctx.Done():
		}
	}
}

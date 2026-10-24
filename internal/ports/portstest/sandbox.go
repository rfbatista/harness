package portstest

import (
	"context"
	"slices"
	"sync"
	"time"

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
	// DriverName is what Name answers; empty: "recording".
	DriverName string
	// Err, if set, is what Wrap answers.
	Err error
	// Block, if set, holds each Follow until it is closed or the Follow's
	// context ends: a log stream that is slow to come up, or never does.
	Block chan struct{}
}

var _ ports.SandboxDriver = (*RecordingSandboxDriver)(nil)

func (d *RecordingSandboxDriver) Name() string {
	if d.DriverName != "" {
		return d.DriverName
	}
	return "recording"
}

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
	// Block, if set, holds each Follow until it is closed or the Follow's
	// context ends: a log stream that is slow to come up, or never does.
	Block chan struct{}
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
	if f.Block != nil {
		select {
		case <-f.Block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
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
//
// A session follows in the background, so Emit first waits (up to a second)
// for a Follow of workingDir to have been made.
func (f *FakeViolationSource) Emit(workingDir string, v ports.SandboxViolation) {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		f.mu.Lock()
		seen := slices.ContainsFunc(f.follows, func(fl *fakeFollow) bool { return fl.dir == workingDir })
		f.mu.Unlock()
		if seen {
			break
		}
	}
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

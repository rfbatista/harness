package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// A read-only turn (the architecture assistant's) reads one checkout and writes
// nothing of the person's: the checkout is readable, never writable, the turn
// starts in it, and the project's other checkouts stay hidden.
func TestReadOnlyWrap_TheCheckoutIsReadableNeverWritableAndTheTurnStartsInIt(t *testing.T) {
	sb, d, home := oneShotSandbox(t)
	checkout, other := "/work/harness", "/work/other"
	sb.Checkouts = func() []string { return []string{checkout, other} }
	claudeDir := "/opt/claude/versions"
	wrap, err := sb.ReadOnlyWrap(checkout, []ports.SandboxPath{{Path: claudeDir}})
	if err != nil || wrap == nil {
		t.Fatalf("ReadOnlyWrap = %v, %v; want a wrap", wrap, err)
	}
	if _, err := wrap(commandIn(checkout)); err != nil {
		t.Fatal(err)
	}
	p := d.Calls()[0].Policy
	if p.WorkingDir != checkout {
		t.Errorf("WorkingDir = %q, want the checkout", p.WorkingDir)
	}
	if !slices.Contains(p.Readable, checkout) || slices.Contains(p.Writable, checkout) {
		t.Errorf("readable %v, writable %v: the checkout must be read-only", p.Readable, p.Writable)
	}
	if !slices.Contains(p.Writable, home) || !slices.Contains(p.Writable, filepath.Clean(os.TempDir())) {
		t.Errorf("writable = %v, want the agent home (the transcript) and the temp dir", p.Writable)
	}
	if !slices.Contains(p.Readable, claudeDir) {
		t.Errorf("readable = %v, want claude's install dir", p.Readable)
	}
	if !slices.Contains(p.Hidden, other) || slices.Contains(p.Hidden, checkout) {
		t.Errorf("hidden = %v, want only the other checkout", p.Hidden)
	}
}

func TestReadOnlyWrap_SwitchedOffThereIsNoWrapAndOnWithoutADriverItIsRefused(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	sb.Enabled = func() bool { return false }
	if w, err := sb.ReadOnlyWrap("/work/x", nil); w != nil || err != nil {
		t.Fatalf("ReadOnlyWrap = %v, %v; want nil, nil with the sandbox off", w, err)
	}
	if n := len(d.Calls()); n != 0 {
		t.Errorf("driver called %d times with the sandbox off", n)
	}
	sb.Enabled = nil
	sb.Driver = nil
	if _, err := sb.ReadOnlyWrap("/work/x", nil); err == nil || !strings.Contains(err.Error(), "sandbox driver") {
		t.Fatalf("err = %v, want the missing-driver refusal", err)
	}
}

func TestReadOnlyWrap_ADriverErrorIsTheWrapError(t *testing.T) {
	sb, d, _ := oneShotSandbox(t)
	d.Err = errors.New("cannot confine")
	wrap, _ := sb.ReadOnlyWrap("/work/x", nil)
	if _, err := wrap(commandIn("/work/x")); err == nil || !strings.Contains(err.Error(), "cannot confine") {
		t.Fatalf("err = %v, want the driver's error", err)
	}
}

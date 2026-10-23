package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// fakeOneShotClaude is a stand-in claude: it records its env and cwd to
// $ONESHOT_RECORD (set through the credential env) and answers a result.
const fakeOneShotClaude = `#!/bin/sh
printf 'bin=%s\ncwd=%s\nconfig=%s\ntoken=%s\n' "$0" "$(pwd)" "$CLAUDE_CONFIG_DIR" "$CLAUDE_CODE_OAUTH_TOKEN" > "$(dirname "$0")/record"
printf '{"subtype":"success","is_error":false,"result":"the gist"}'
`

type oneShotWorld struct {
	bin, home string
	driver    *portstest.RecordingSandboxDriver
	sb        *orchestration.Sandbox
	off       bool
	text      claudetext.OneShotter
}

func newOneShotWorld(t *testing.T) *oneShotWorld {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(fakeOneShotClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	w := &oneShotWorld{bin: bin, home: t.TempDir(), driver: &portstest.RecordingSandboxDriver{}}
	w.sb = &orchestration.Sandbox{Driver: w.driver, Enabled: func() bool { return !w.off }, Home: w.home}
	w.text = claudetext.Confined(
		oneShotClaude(bin, w.home, ""),
		oneShotPrep(w.sb, func() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-1"} }, bin),
	)
	return w
}

func (w *oneShotWorld) summarize(t *testing.T) error {
	t.Helper()
	_, err := claudetext.NewSummarizer(w.text, w.bin, "").Summarize(context.Background(), "some text")
	return err
}

func TestOneShot_RunsManagedClaudeUnderAPolicyWithNoRepositoryWithTheCredentialAndAgentHome(t *testing.T) {
	w := newOneShotWorld(t)
	if err := w.summarize(t); err != nil {
		t.Fatal(err)
	}
	calls := w.driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("driver called %d times, want 1", len(calls))
	}
	c := calls[0]
	if c.Command.Path != w.bin {
		t.Errorf("wrapped command = %q, want the managed claude %q", c.Command.Path, w.bin)
	}
	if c.Command.Dir != os.TempDir() || c.Policy.WorkingDir != os.TempDir() {
		t.Errorf("cwd = %q / policy %q, want the temp dir", c.Command.Dir, c.Policy.WorkingDir)
	}
	if !slices.Contains(c.Command.Env, "CLAUDE_CODE_OAUTH_TOKEN=tok-1") || !slices.Contains(c.Command.Env, "CLAUDE_CONFIG_DIR="+w.home) {
		t.Errorf("env lacks the credential or the agent home")
	}
	if !slices.Contains(c.Policy.Writable, w.home) || !slices.Contains(c.Policy.Readable, filepath.Dir(w.bin)) {
		t.Errorf("policy = %+v, want the agent home writable and claude's dir readable", c.Policy)
	}
	resolvedTmp, _ := filepath.EvalSymlinks(os.TempDir())
	resolvedBin, _ := filepath.EvalSymlinks(w.bin)
	for _, p := range slices.Concat(c.Policy.Readable, c.Policy.Writable) {
		switch {
		case p == w.home, p == filepath.Clean(os.TempDir()), p == resolvedTmp:
		case strings.HasPrefix(w.bin, p+"/"), strings.HasPrefix(resolvedBin, p+"/"): // claude's install dirs
		default:
			t.Errorf("policy names %q: a one-shot sees no repository or toolchain", p)
		}
	}
	rec, _ := os.ReadFile(filepath.Join(filepath.Dir(w.bin), "record"))
	if !strings.Contains(string(rec), "token=tok-1") || !strings.Contains(string(rec), "config="+w.home) {
		t.Errorf("the claude saw %q, want the credential and the agent home", rec)
	}
}

func TestOneShot_SandboxOffRunsAsBeforeWithNoDriverCall(t *testing.T) {
	w := newOneShotWorld(t)
	w.off = true
	if err := w.summarize(t); err != nil {
		t.Fatal(err)
	}
	if n := len(w.driver.Calls()); n != 0 {
		t.Fatalf("driver called %d times with the sandbox off", n)
	}
	w.off = false // read per call
	if err := w.summarize(t); err != nil || len(w.driver.Calls()) != 1 {
		t.Fatalf("err = %v, calls = %d; want a wrapped call once the sandbox is back on", err, len(w.driver.Calls()))
	}
}

func TestOneShot_ADriverErrorFailsTheOneShotWithIt(t *testing.T) {
	w := newOneShotWorld(t)
	w.driver.Err = errors.New("cannot confine")
	err := w.summarize(t)
	if err == nil || !strings.Contains(err.Error(), "claude: wrap command") || !strings.Contains(err.Error(), "cannot confine") {
		t.Fatalf("err = %v, want the wrap error carrying the driver's", err)
	}
}

func TestOneShot_OnWithoutADriverIsRefusedNamingTheDriver(t *testing.T) {
	w := newOneShotWorld(t)
	w.sb.Driver = nil
	err := w.summarize(t)
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != agentready.CodeSandboxMissing {
		t.Fatalf("err = %v, want %s", err, agentready.CodeSandboxMissing)
	}
}

func TestOneShot_AMissingCredentialIsRefusedByTheGateBeforeAnythingRuns(t *testing.T) {
	w := newOneShotWorld(t)
	gate := &agentready.Gate{Credential: func() bool { return false }}
	w.text = claudetext.Gated(w.text, gate.Check)
	err := w.summarize(t)
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != agentready.CodeCredentialMissing {
		t.Fatalf("err = %v, want %s", err, agentready.CodeCredentialMissing)
	}
	if n := len(w.driver.Calls()); n != 0 {
		t.Errorf("driver called %d times for a refused one-shot", n)
	}
}

var _ ports.SandboxDriver = (*portstest.RecordingSandboxDriver)(nil)

//go:build !windows

package agentoauth_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeSetupToken stands in for `claude setup-token`: it prints an
// authorization URL, waits for the code on stdin and prints a token for the
// right one. It records where it ran, so the tests can see the config dir.
const fakeSetupToken = `#!/bin/sh
dir=$(dirname "$0")
[ "$1" = setup-token ] || exit 2
echo $$ > "$dir/pid"
echo "$CLAUDE_CONFIG_DIR" > "$dir/config-dir"
echo "Open this URL to sign in:"
echo "https://claude.ai/oauth/authorize?code=true&client_id=fake&state=abc123"
printf "Paste the code here: "
read code
if [ "$code" = "good-code" ]; then
  echo
  echo "Your OAuth token (valid for 1 year):"
  echo "sk-ant-oat01-faketokenvalue"
  exit 0
fi
if [ "$FAKE_MODE" = tui ]; then
  printf '\033[>4m\033[<u'
  exit 1
fi
if [ "$FAKE_MODE" = leaky ]; then
  echo "failed with sk-ant-oat01-leaked and $code"
  exit 1
fi
echo "invalid code"
exit 1
`

const person = "/home/person/.claude"

type bin struct{ path string }

func (b bin) Bin() string      { return b.path }
func (b bin) Installed() bool  { return b.path != "" }
func (b bin) Overridden() bool { return false }

type memStore struct {
	mu  sync.Mutex
	rec *ports.AgentCredentialRecord
}

func (m *memStore) Get() (*ports.AgentCredentialRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rec, nil
}
func (m *memStore) Put(r ports.AgentCredentialRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rec = &r
	return nil
}
func (m *memStore) Delete() error { m.mu.Lock(); defer m.mu.Unlock(); m.rec = nil; return nil }

type plainBox struct{}

func (plainBox) KeyID() string { return "k" }
func (plainBox) Seal(p []byte, _ bool) ([]byte, string, error) {
	return append([]byte("sealed:"), p...), "k", nil
}
func (plainBox) Open(s []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(s), "sealed:")), nil
}

// clock is a hand-turned clock: After hands out a channel the test fires.
type clock struct {
	mu   sync.Mutex
	now  time.Time
	fire chan time.Time
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), fire: make(chan time.Time, 1)}
}
func (c *clock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) After(time.Duration) <-chan time.Time { return c.fire }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	t := c.now
	c.mu.Unlock()
	c.fire <- t
}

type rig struct {
	svc  *agentoauth.Service
	cred *agentcredential.Service
	clk  *clock
	dir  string
	home string
}

func newRig(t *testing.T, script string) *rig {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", person)
	dir := t.TempDir()
	path := ""
	if script != "" {
		path = filepath.Join(dir, "claude")
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(dir, "agent-home")
	clk := newClock()
	cred := agentcredential.New(&memStore{}, plainBox{}, clk.Now)
	svc := agentoauth.New(agentoauth.Deps{Bin: bin{path}, ConfigDir: home, PTY: ptyunix.New(), Credential: cred, Now: clk.Now, After: clk.After})
	t.Cleanup(svc.Close)
	return &rig{svc: svc, cred: cred, clk: clk, dir: dir, home: home}
}

func (r *rig) waitPhase(t *testing.T, phase string) ports.AgentOAuthState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		st := r.svc.State()
		if st.Phase == phase {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("flow never reached %q, is at %+v", phase, st)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *rig) pid(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func gone(pid int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func code(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const authURL = "https://claude.ai/oauth/authorize?code=true&client_id=fake&state=abc123"

func TestStartingTheFlowShowsTheAuthorizationURLAndRunsInTheAgentClaudeHome(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	st, err := r.svc.Start()
	if err != nil || st.Phase == ports.AgentOAuthIdle {
		t.Fatalf("start = %+v, %v", st, err)
	}
	st = r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if st.URL != authURL {
		t.Errorf("url = %q", st.URL)
	}
	if st.RemainingSeconds != 600 {
		t.Errorf("remaining = %d, want 600", st.RemainingSeconds)
	}
	got, _ := os.ReadFile(filepath.Join(r.dir, "config-dir"))
	if strings.TrimSpace(string(got)) != r.home {
		t.Errorf("setup-token ran with CLAUDE_CONFIG_DIR=%q, want the agent home %q", got, r.home)
	}
	r.clk.mu.Lock()
	r.clk.now = r.clk.now.Add(90 * time.Second)
	r.clk.mu.Unlock()
	if rem := r.svc.State().RemainingSeconds; rem != 510 {
		t.Errorf("remaining after 90s = %d, want 510", rem)
	}
}

func TestSubmittingTheCodeStoresTheTokenAsTheOAuthCredentialAndNeverShowsIt(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	waiting := r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Submit("good-code"); err != nil {
		t.Fatal(err)
	}
	done := r.waitPhase(t, ports.AgentOAuthDone)
	if st := r.cred.Status(); !st.Set || st.Kind != ports.AgentCredentialOAuth {
		t.Fatalf("credential = %+v", st)
	}
	if env := r.cred.Env(); len(env) != 1 || env[0] != "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-faketokenvalue" {
		t.Fatalf("env = %v", env)
	}
	for _, st := range []ports.AgentOAuthState{waiting, done, r.svc.State()} {
		if j := asJSON(t, st); strings.Contains(j, "faketokenvalue") || strings.Contains(j, "good-code") {
			t.Errorf("state carries a secret: %s", j)
		}
	}
	if !gone(r.pid(t)) {
		t.Error("the process outlived the flow")
	}
}

func TestASecondStartWhileOneRunsIsRefused(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Start(); code(err) != "AGENT_OAUTH_BUSY" {
		t.Fatalf("second start = %v, want AGENT_OAUTH_BUSY", err)
	}
	if st := r.svc.State(); st.Phase != ports.AgentOAuthWaitingForCode || st.URL != authURL {
		t.Errorf("the running flow changed: %+v", st)
	}
}

func TestCancelKillsTheProcessAndLeavesThePreviousCredentialUntouched(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	if _, err := r.cred.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-before"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	pid := r.pid(t)
	if st := r.svc.Cancel(); st.Phase != ports.AgentOAuthIdle {
		t.Fatalf("after cancel: %+v", st)
	}
	if !gone(pid) {
		t.Error("cancel left the process running")
	}
	if env := r.cred.Env(); len(env) != 1 || env[0] != "ANTHROPIC_API_KEY=sk-ant-api03-before" {
		t.Errorf("credential changed: %v", env)
	}
	if _, err := r.svc.Start(); err != nil {
		t.Errorf("a new flow after cancel: %v", err)
	}
}

func TestAFailingProcessEndsTheFlowWithItsLastLineAndChangesNothing(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	_, _ = r.cred.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-before")
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Submit("bad-code"); err != nil {
		t.Fatal(err)
	}
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if !strings.Contains(st.Message, "Nothing was saved") || st.Detail != "invalid code" {
		t.Errorf("failed state = %+v", st)
	}
	if env := r.cred.Env(); len(env) != 1 || env[0] != "ANTHROPIC_API_KEY=sk-ant-api03-before" {
		t.Errorf("credential changed: %v", env)
	}
	if _, err := r.svc.Start(); err != nil {
		t.Errorf("start again after a failure: %v", err)
	}
}

func TestKeyboardModeResetsAreNotTheFailureDetail(t *testing.T) {
	t.Setenv("FAKE_MODE", "tui")
	r := newRig(t, fakeSetupToken)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("bad-code")
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if st.Detail == "" || strings.ContainsAny(st.Detail, "[<>\x1b") {
		t.Errorf("detail = %q", st.Detail)
	}
}

func TestTheLastLineNeverCarriesATokenOrTheCode(t *testing.T) {
	t.Setenv("FAKE_MODE", "leaky")
	r := newRig(t, fakeSetupToken)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("secret-code-123")
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if j := asJSON(t, st); strings.Contains(j, "leaked") || strings.Contains(j, "secret-code-123") {
		t.Errorf("failure detail leaks: %s", j)
	}
}

func TestTheFlowTimesOutWithAClearMessageAndKillsTheProcess(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	_, _ = r.cred.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-before")
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	pid := r.pid(t)
	r.clk.advance(10 * time.Minute)
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if st.Message != "The sign-in timed out. Nothing was saved." {
		t.Errorf("message = %q", st.Message)
	}
	if !gone(pid) {
		t.Error("timeout left the process running")
	}
	if len(r.cred.Env()) != 1 || !strings.HasPrefix(r.cred.Env()[0], "ANTHROPIC_API_KEY=") {
		t.Errorf("credential changed: %v", r.cred.Env())
	}
}

func TestClosingTheServiceKillsTheProcess(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	pid := r.pid(t)
	r.svc.Close()
	if !gone(pid) {
		t.Error("the process outlived the service")
	}
}

func TestWithoutAManagedClaudeTheFlowDoesNotStart(t *testing.T) {
	r := newRig(t, "")
	if _, err := r.svc.Start(); code(err) != "CLAUDE_CLI_NOT_FOUND" {
		t.Fatalf("start = %v, want CLAUDE_CLI_NOT_FOUND", err)
	}
	if r.svc.State().Phase != ports.AgentOAuthIdle {
		t.Errorf("state = %+v", r.svc.State())
	}
}

func TestACodeNeedsAWaitingFlow(t *testing.T) {
	r := newRig(t, fakeSetupToken)
	if _, err := r.svc.Submit("x"); code(err) != "AGENT_OAUTH_NOT_WAITING" {
		t.Errorf("submit without a flow = %v", err)
	}
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Submit("  "); code(err) != "INVALID_INPUT" {
		t.Errorf("empty code = %v", err)
	}
}

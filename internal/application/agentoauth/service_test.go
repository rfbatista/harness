//go:build !windows

package agentoauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeClaude stands in for the managed claude. `auth login --claudeai`
// prints an authorization URL, waits for the code on stdin and, for the right
// one, keeps a login in CLAUDE_CONFIG_DIR as the real claude does; `auth
// status` prints JSON from that file. It records where it ran, so the tests
// can see the config dir and the env.
const fakeClaude = `#!/bin/sh
dir=$(dirname "$0")
if [ "$1 $2" = "auth status" ]; then
  if [ "$FAKE_STATUS" = out ] || [ ! -f "$CLAUDE_CONFIG_DIR/.credentials.json" ]; then
    echo '{"loggedIn": false, "authMethod": "none"}'
    exit 1
  fi
  echo '{"loggedIn": true, "authMethod": "claude.ai", "subscriptionType": "max", "email": "p@example.com"}'
  exit 0
fi
if [ "$1 $2" = "auth logout" ]; then
  pwd > "$dir/logout-cwd"
  if [ "$FAKE_LOGOUT" = fail ]; then
    echo "logout failed: sk-ant-oat01-leaked" >&2
    exit 1
  fi
  rm -f "$CLAUDE_CONFIG_DIR/.credentials.json"
  exit 0
fi
[ "$1 $2 $3" = "auth login --claudeai" ] || exit 2
echo $$ > "$dir/pid"
echo "$CLAUDE_CONFIG_DIR" > "$dir/config-dir"
env > "$dir/env"
pwd > "$dir/cwd"
echo "Opening browser to sign in..."
echo "If the browser didn't open, visit: https://claude.ai/oauth/authorize?code=true&client_id=fake&state=abc123"
printf "Paste code here if prompted > "
read code
if [ "$code" = "good-code" ]; then
  echo
  echo '{"claudeAiOauth":{"accessToken":"sk-ant-oat01-faketokenvalue"}}' > "$CLAUDE_CONFIG_DIR/.credentials.json"
  echo "Login successful."
  [ "$FAKE_MODE" = hang ] && sleep 60
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
	svc *agentoauth.Service
	clk *clock
	// wrapped are the commands the driver was handed.
	wrapped *[]ports.SandboxCommand
	dir     string
	home    string
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
	var wrapped []ports.SandboxCommand
	var mu sync.Mutex
	wrap := func(c ports.SandboxCommand) (ports.SandboxCommand, error) {
		mu.Lock()
		defer mu.Unlock()
		wrapped = append(wrapped, c)
		return c, nil
	}
	svc := agentoauth.New(agentoauth.Deps{Bin: bin{path}, ConfigDir: home, PTY: ptyunix.New(), Wrap: wrap, Now: clk.Now, After: clk.After})
	t.Cleanup(svc.Close)
	return &rig{svc: svc, clk: clk, dir: dir, home: home, wrapped: &wrapped}
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
	r := newRig(t, fakeClaude)
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

func TestSubmittingTheCodeEndsTheFlowOnceClaudeExitsCleanlyAndIsSignedIn(t *testing.T) {
	r := newRig(t, fakeClaude)
	if _, err := r.svc.Start(); err != nil {
		t.Fatal(err)
	}
	waiting := r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Submit("good-code"); err != nil {
		t.Fatal(err)
	}
	done := r.waitPhase(t, ports.AgentOAuthDone)
	if st, err := r.svc.Status(context.Background()); err != nil || !st.LoggedIn || st.Email != "p@example.com" || st.SubscriptionType != "max" {
		t.Fatalf("status = %+v, %v", st, err)
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
	r := newRig(t, fakeClaude)
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
	r := newRig(t, fakeClaude)
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
	r.noLogin(t)
	if _, err := r.svc.Start(); err != nil {
		t.Errorf("a new flow after cancel: %v", err)
	}
}

func TestAFailingProcessEndsTheFlowWithItsLastLineAndChangesNothing(t *testing.T) {
	r := newRig(t, fakeClaude)
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
	r.noLogin(t)
	if _, err := r.svc.Start(); err != nil {
		t.Errorf("start again after a failure: %v", err)
	}
}

func TestKeyboardModeResetsAreNotTheFailureDetail(t *testing.T) {
	t.Setenv("FAKE_MODE", "tui")
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("bad-code")
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if st.Detail == "" || strings.ContainsAny(st.Detail, "\x1b") || strings.Contains(st.Detail, "[>4m") || strings.Contains(st.Detail, "[<u") {
		t.Errorf("detail = %q", st.Detail)
	}
}

func TestTheLastLineNeverCarriesATokenOrTheCode(t *testing.T) {
	t.Setenv("FAKE_MODE", "leaky")
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("secret-code-123")
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if j := asJSON(t, st); strings.Contains(j, "leaked") || strings.Contains(j, "secret-code-123") {
		t.Errorf("failure detail leaks: %s", j)
	}
}

func TestTheFlowTimesOutWithAClearMessageAndKillsTheProcess(t *testing.T) {
	r := newRig(t, fakeClaude)
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
	r.noLogin(t)
}

func TestClosingTheServiceKillsTheProcess(t *testing.T) {
	r := newRig(t, fakeClaude)
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
	r := newRig(t, fakeClaude)
	if _, err := r.svc.Submit("x"); code(err) != "AGENT_OAUTH_NOT_WAITING" {
		t.Errorf("submit without a flow = %v", err)
	}
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	if _, err := r.svc.Submit("  "); code(err) != "INVALID_INPUT" {
		t.Errorf("empty code = %v", err)
	}
}

// noLogin fails when a login was kept in the agent home.
func (r *rig) noLogin(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(r.home, ".credentials.json")); err == nil {
		t.Error("a login was kept although the flow did not finish")
	}
}

func TestACleanExitThatIsNotSignedInEndsTheFlowWithoutALogin(t *testing.T) {
	t.Setenv("FAKE_STATUS", "out")
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("good-code")
	st := r.waitPhase(t, ports.AgentOAuthFailed)
	if !strings.Contains(st.Message, "Nothing was saved") {
		t.Errorf("failed state = %+v", st)
	}
	if !gone(r.pid(t)) {
		t.Error("the process outlived the flow")
	}
	if st, err := r.svc.Status(context.Background()); err != nil || st.LoggedIn {
		t.Errorf("status = %+v, %v", st, err)
	}
}

func TestCancelDuringTheStatusCheckEndsTheFlow(t *testing.T) {
	t.Setenv("FAKE_MODE", "hang")
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("good-code")
	time.Sleep(300 * time.Millisecond)
	if st := r.svc.Cancel(); st.Phase != ports.AgentOAuthIdle {
		t.Fatalf("after cancel: %+v", st)
	}
	if !gone(r.pid(t)) {
		t.Error("cancel left the process running")
	}
}

func TestTheLoginRunsUnderTheDriverInTheAgentHomeWithNoCredentialVariables(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-person")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-person")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "person-auth")
	t.Setenv("BROWSER", "firefox")
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	cmds := *r.wrapped
	if len(cmds) != 1 {
		t.Fatalf("driver was handed %d commands, want the login", len(cmds))
	}
	if got := strings.Join(cmds[0].Args, " "); got != "auth login --claudeai" {
		t.Errorf("args = %q", got)
	}
	env, _ := os.ReadFile(filepath.Join(r.dir, "env"))
	for _, bad := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN", "sk-ant", "firefox"} {
		if strings.Contains(string(env), bad) {
			t.Errorf("the login's env carries %s", bad)
		}
	}
	for _, want := range []string{"CLAUDE_CONFIG_DIR=" + r.home, "BROWSER=/usr/bin/true"} {
		if !strings.Contains(string(env), want+"\n") {
			t.Errorf("the login's env lacks %s", want)
		}
	}
	tmp := ""
	for _, kv := range cmds[0].Env {
		if v, ok := strings.CutPrefix(kv, "TMPDIR="); ok {
			tmp = v
		}
	}
	if tmp == "" || tmp == os.TempDir() {
		t.Errorf("the login has no temp dir of its own: %q", tmp)
	}
	if cmds[0].Dir != r.home {
		t.Errorf("dir = %q, want the agent home", cmds[0].Dir)
	}
	r.svc.Cancel()
	if !gone(r.pid(t)) {
		t.Fatal("process outlived the cancel")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(tmp); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the login's temp dir was left behind")
}

// Logout runs `claude auth logout` the way the login runs, and what the status
// says afterwards is the answer: the login is gone.
func TestLogoutRunsUnderTheDriverInTheAgentHomeAndSignsTheHomeOut(t *testing.T) {
	r := newRig(t, fakeClaude)
	_, _ = r.svc.Start()
	r.waitPhase(t, ports.AgentOAuthWaitingForCode)
	_, _ = r.svc.Submit("good-code")
	r.waitPhase(t, ports.AgentOAuthDone)
	if st, err := r.svc.Status(context.Background()); err != nil || !st.LoggedIn {
		t.Fatalf("before logout: %+v, %v", st, err)
	}
	if err := r.svc.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.noLogin(t)
	if st, err := r.svc.Status(context.Background()); err != nil || st.LoggedIn {
		t.Errorf("after logout: %+v, %v", st, err)
	}
	var logout *ports.SandboxCommand
	for _, c := range *r.wrapped {
		if strings.Join(c.Args, " ") == "auth logout" {
			logout = &c
		}
	}
	if logout == nil {
		t.Fatalf("the driver was not handed the logout: %+v", *r.wrapped)
	}
	if logout.Dir != r.home {
		t.Errorf("logout dir = %q, want the agent home", logout.Dir)
	}
	if !slices.Contains(logout.Env, "CLAUDE_CONFIG_DIR="+r.home) {
		t.Errorf("logout env lacks the agent home")
	}
}

func TestAFailingLogoutIsAnErrorWithItsLastLineAndNoToken(t *testing.T) {
	t.Setenv("FAKE_LOGOUT", "fail")
	r := newRig(t, fakeClaude)
	err := r.svc.Logout(context.Background())
	if code(err) != "AGENT_SIGN_OUT_FAILED" {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sk-ant") || !strings.Contains(err.Error(), "logout failed") {
		t.Errorf("message = %q", err)
	}
}

func TestLogoutWithoutAManagedClaudeIsRefused(t *testing.T) {
	r := newRig(t, "")
	if code(r.svc.Logout(context.Background())) != "CLAUDE_CLI_NOT_FOUND" {
		t.Error("logout ran without a claude")
	}
}

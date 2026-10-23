// Package agentoauth signs agents in with the person's Claude subscription
// (ADR 0022): it runs `claude auth login --claudeai` with the managed claude
// in the agent Claude home, on a terminal, under the sandbox driver with the
// agent-home-only policy (so claude cannot reach the person's keychain and
// keeps the login as .credentials.json in the agent home). It hands the
// authorization URL to the person and writes the code they paste to the
// process; the flow is done when claude exits cleanly and `claude auth
// status` reports a signed-in account. The harness stores no token. One flow
// runs at a time and its state lives here, so a page that was left finds it
// again. The code never leaves the package except into the process.
package agentoauth

import (
	"bytes"
	"context"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// DefaultTimeout is how long a flow may wait for the person.
const DefaultTimeout = 10 * time.Minute

// enterDelay is how long the code sits in the prompt before Enter is pressed.
const enterDelay = 150 * time.Millisecond

var (
	_ ports.AgentOAuth = (*Service)(nil)
	_ ports.AgentAuth  = (*Service)(nil)
)

// Deps are what the flow runs on.
type Deps struct {
	// Bin says which claude to run and whether it can run.
	Bin ports.ManagedClaudeBin
	// ConfigDir is the agent Claude home, passed as CLAUDE_CONFIG_DIR.
	ConfigDir string
	// PTY runs the login: claude wants a terminal.
	PTY ports.PTY
	// Env is the environment the process starts from; nil is os.Environ. The
	// variables that would sign it in or move its home are dropped.
	Env func() []string
	// Wrap confines the login and the status under the sandbox driver; nil
	// runs them unwrapped.
	Wrap Wrap
	// Now and After are the clock; nil are the system's.
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
	// Timeout is how long a flow lives; zero is DefaultTimeout.
	Timeout time.Duration
}

// Service is the OAuth flow.
type Service struct {
	d    Deps
	auth *Auth

	mu     sync.Mutex
	cur    *flow
	result ports.AgentOAuthState // the last flow's end: done or failed
	closed bool
	wg     sync.WaitGroup
}

// flow is the process that runs and what it printed so far.
type flow struct {
	proc     ports.PTYProcess
	phase    string
	url      string
	deadline time.Time
	out      bytes.Buffer
	code     string
	done     chan struct{}
	// ctx ends with the flow: it stops a status check that runs.
	ctx    context.Context
	cancel context.CancelFunc
	// cleanup removes the process's private temp dir.
	cleanup func()
}

// New builds the service.
func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.After == nil {
		d.After = time.After
	}
	if d.Timeout <= 0 {
		d.Timeout = DefaultTimeout
	}
	if d.Env == nil {
		d.Env = os.Environ
	}
	return &Service{d: d, auth: NewAuth(AuthDeps{Bin: d.Bin, ConfigDir: d.ConfigDir, Env: d.Env, Wrap: d.Wrap})}
}

// Status is `claude auth status` in the agent Claude home, run the way the
// login runs.
func (s *Service) Status(ctx context.Context) (ports.AgentAuthStatus, error) {
	return s.auth.Status(ctx)
}

// Logout is `claude auth logout` in the agent Claude home, run the way the
// login runs.
func (s *Service) Logout(ctx context.Context) error {
	return s.auth.Logout(ctx)
}

func (s *Service) State() ports.AgentOAuthState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *Service) stateLocked() ports.AgentOAuthState {
	f := s.cur
	if f == nil {
		if s.result.Phase == "" {
			return ports.AgentOAuthState{Phase: ports.AgentOAuthIdle}
		}
		return s.result
	}
	remaining := int(f.deadline.Sub(s.d.Now()) / time.Second)
	return ports.AgentOAuthState{Phase: f.phase, URL: f.url, RemainingSeconds: max(0, remaining)}
}

func (s *Service) Start() (ports.AgentOAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil || s.closed {
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "AGENT_OAUTH_BUSY", Message: "A sign-in is already open. Finish or cancel it first."}
	}
	spec, cleanup, err := s.auth.spec("auth", "login", "--claudeai")
	if err != nil {
		if code(err) != "" {
			return ports.AgentOAuthState{}, err
		}
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND", Message: "Could not run the managed claude: " + err.Error()}
	}
	proc, err := s.d.PTY.Start(spec, ports.TermSize{Cols: 4000, Rows: 24})
	if err != nil {
		cleanup()
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND", Message: "Could not run the managed claude: " + err.Error()}
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &flow{proc: proc, phase: ports.AgentOAuthStarting, deadline: s.d.Now().Add(s.d.Timeout), done: make(chan struct{}), ctx: ctx, cancel: cancel, cleanup: cleanup}
	s.cur, s.result = f, ports.AgentOAuthState{}
	timeout := s.d.After(s.d.Timeout)
	s.wg.Add(2)
	go s.read(f)
	go s.watch(f, timeout)
	return s.stateLocked(), nil
}

func (s *Service) Submit(code string) (ports.AgentOAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.cur
	if f == nil || f.phase != ports.AgentOAuthWaitingForCode {
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "AGENT_OAUTH_NOT_WAITING", Message: "No sign-in is waiting for a code. Start one first."}
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "Paste the code Claude showed."}
	}
	f.code = code
	f.phase = ports.AgentOAuthChecking
	// The code and the Enter go as two writes: claude's terminal UI reads a
	// burst that ends in a newline as a paste, and a paste does not submit.
	if _, err := f.proc.Write([]byte(code)); err != nil {
		s.endLocked(f, ports.AgentOAuthState{Phase: ports.AgentOAuthFailed, Message: "The code could not be sent to Claude. Nothing was saved."})
		return s.stateLocked(), nil
	}
	s.wg.Add(1)
	go s.enter(f)
	return s.stateLocked(), nil
}

// enter presses Enter once the pasted code has settled in the prompt.
func (s *Service) enter(f *flow) {
	defer s.wg.Done()
	select {
	case <-time.After(enterDelay):
	case <-f.done:
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != f {
		return
	}
	if _, err := f.proc.Write([]byte("\r")); err != nil {
		s.endLocked(f, ports.AgentOAuthState{Phase: ports.AgentOAuthFailed, Message: "The code could not be sent to Claude. Nothing was saved."})
	}
}

func (s *Service) Cancel() ports.AgentOAuthState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil {
		s.endLocked(s.cur, ports.AgentOAuthState{Phase: ports.AgentOAuthIdle})
	}
	s.result = ports.AgentOAuthState{}
	return ports.AgentOAuthState{Phase: ports.AgentOAuthIdle}
}

// Close ends the flow that runs and waits until its process is reaped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	if s.cur != nil {
		s.endLocked(s.cur, ports.AgentOAuthState{Phase: ports.AgentOAuthIdle})
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// endLocked ends f with result and kills its process; the reader reaps it.
func (s *Service) endLocked(f *flow, result ports.AgentOAuthState) {
	if s.cur != f {
		return
	}
	s.cur, s.result = nil, result
	f.cancel()
	_ = f.proc.Signal(os.Kill)
}

// watch ends the flow when its time is up.
func (s *Service) watch(f *flow, timeout <-chan time.Time) {
	defer s.wg.Done()
	select {
	case <-timeout:
		s.mu.Lock()
		s.endLocked(f, ports.AgentOAuthState{Phase: ports.AgentOAuthFailed, Message: "The sign-in timed out. Nothing was saved."})
		s.mu.Unlock()
	case <-f.done:
	}
}

// read follows the process's output until it ends, then reaps it and settles
// the flow if nobody ended it first.
func (s *Service) read(f *flow) {
	defer s.wg.Done()
	buf := make([]byte, 4096)
	for {
		n, err := f.proc.Read(buf)
		if n > 0 {
			s.output(f, buf[:n])
		}
		if err != nil {
			break
		}
	}
	exit, _ := f.proc.Wait()
	_ = f.proc.Close()
	f.cleanup()
	// A clean exit is only the first half: what claude says about the home
	// decides, asked outside the lock (it takes a moment, and a cancel ends
	// it through f.ctx).
	signedIn := false
	if exit == 0 {
		st, err := s.auth.Status(f.ctx)
		signedIn = err == nil && st.LoggedIn
	}
	s.mu.Lock()
	s.settleLocked(f, exit, signedIn)
	s.mu.Unlock()
	f.cancel()
	close(f.done)
}

func (s *Service) output(f *flow, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.out.Write(p)
	if f.phase != ports.AgentOAuthStarting || s.cur != f {
		return
	}
	if m := urlRe.FindStringSubmatch(clean(f.out.String())); m != nil {
		f.url, f.phase = m[1], ports.AgentOAuthWaitingForCode
	}
}

// settleLocked decides a flow whose process ended on its own: a clean exit
// with a signed-in status is done, anything else a failure that saved nothing.
func (s *Service) settleLocked(f *flow, exit int, signedIn bool) {
	if s.cur != f {
		return
	}
	if exit == 0 && signedIn {
		s.cur, s.result = nil, ports.AgentOAuthState{Phase: ports.AgentOAuthDone}
		return
	}
	text := clean(f.out.String())
	detail := lastLine(text, f.code)
	if detail == "" {
		detail = "claude exited with code " + strconv.Itoa(exit) + " without signing in"
	}
	if exit == 0 {
		detail = "claude says no account is signed in"
	}
	s.cur, s.result = nil, ports.AgentOAuthState{
		Phase:   ports.AgentOAuthFailed,
		Message: "The sign-in did not finish. Nothing was saved.",
		Detail:  detail,
	}
}

var (
	urlRe   = regexp.MustCompile(`(https?://[^\s<>]+)[\s>]`)
	tokenRe = regexp.MustCompile(`sk-ant-oat[A-Za-z0-9_-]+`)
	ansiRe  = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]`)
)

// clean is terminal output as plain lines.
func clean(s string) string {
	return strings.ReplaceAll(ansiRe.ReplaceAllString(s, ""), "\r", "")
}

// lastLine is the last non-blank line, with any token and the pasted code
// blotted out; it is short enough to show.
func lastLine(text, code string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		line = tokenRe.ReplaceAllString(line, "…")
		if code != "" {
			line = strings.ReplaceAll(line, code, "…")
		}
		if r := []rune(line); len(r) > 200 {
			line = string(r[:200]) + "…"
		}
		return line
	}
	return ""
}

// code is the StructuredError code of err, empty for any other error.
func code(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}

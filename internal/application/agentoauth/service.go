// Package agentoauth signs agents in with the person's Claude subscription
// (ADR 0018): it runs `claude setup-token` with the managed claude in the
// agent Claude home, on a terminal, hands the authorization URL to the
// person, writes the code they paste to the process and stores the token it
// prints as the agent credential. One flow runs at a time and its state lives
// here, so a page that was left finds it again. The token and the code never
// leave the package except into the credential store and the process.
package agentoauth

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// DefaultTimeout is how long a flow may wait for the person.
const DefaultTimeout = 10 * time.Minute

var _ ports.AgentOAuth = (*Service)(nil)

// Deps are what the flow runs on.
type Deps struct {
	// Bin says which claude to run and whether it can run.
	Bin ports.ManagedClaudeBin
	// ConfigDir is the agent Claude home, passed as CLAUDE_CONFIG_DIR.
	ConfigDir string
	// PTY runs the process: setup-token wants a terminal.
	PTY ports.PTY
	// Credential receives the token.
	Credential ports.AgentCredential
	// Env is the environment the process starts from; nil is os.Environ. The
	// variables that would sign it in or move its home are dropped.
	Env func() []string
	// Now and After are the clock; nil are the system's.
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
	// Timeout is how long a flow lives; zero is DefaultTimeout.
	Timeout time.Duration
}

// Service is the OAuth flow.
type Service struct {
	d Deps

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
	return &Service{d: d}
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
	if s.d.Bin == nil || !s.d.Bin.Installed() {
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND", Message: "Sign in needs the managed claude. Install it first."}
	}
	if err := os.MkdirAll(s.d.ConfigDir, 0o700); err != nil {
		return ports.AgentOAuthState{}, err
	}
	proc, err := s.d.PTY.Start(ports.ProcessSpec{
		Path: s.d.Bin.Bin(),
		Args: []string{"setup-token"},
		Dir:  s.d.ConfigDir,
		Env:  s.env(),
	}, ports.TermSize{Cols: 4000, Rows: 24})
	if err != nil {
		return ports.AgentOAuthState{}, &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND", Message: "Could not run the managed claude: " + err.Error()}
	}
	f := &flow{proc: proc, phase: ports.AgentOAuthStarting, deadline: s.d.Now().Add(s.d.Timeout), done: make(chan struct{})}
	s.cur, s.result = f, ports.AgentOAuthState{}
	timeout := s.d.After(s.d.Timeout)
	s.wg.Add(2)
	go s.read(f)
	go s.watch(f, timeout)
	return s.stateLocked(), nil
}

// env is the process's environment: the base without anything that signs in
// or moves the Claude home, then the agent Claude home.
func (s *Service) env() []string {
	var env []string
	for _, kv := range s.d.Env() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "CLAUDE_CONFIG_DIR="+s.d.ConfigDir)
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
	if _, err := f.proc.Write([]byte(code + "\r")); err != nil {
		s.endLocked(f, ports.AgentOAuthState{Phase: ports.AgentOAuthFailed, Message: "The code could not be sent to Claude. Nothing was saved."})
	}
	return s.stateLocked(), nil
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
	s.mu.Lock()
	s.settleLocked(f, exit)
	s.mu.Unlock()
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

// settleLocked decides a flow whose process ended on its own: a token after
// a clean exit is stored, anything else is a failure that saves nothing.
func (s *Service) settleLocked(f *flow, exit int) {
	if s.cur != f {
		return
	}
	text := clean(f.out.String())
	token := tokenRe.FindString(text)
	if f.phase == ports.AgentOAuthChecking && exit == 0 && token != "" {
		if _, err := s.d.Credential.Set(ports.AgentCredentialOAuth, token); err != nil {
			s.cur, s.result = nil, ports.AgentOAuthState{Phase: ports.AgentOAuthFailed, Message: "The token could not be saved. Nothing was saved."}
			return
		}
		s.cur, s.result = nil, ports.AgentOAuthState{Phase: ports.AgentOAuthDone}
		return
	}
	s.cur, s.result = nil, ports.AgentOAuthState{
		Phase:   ports.AgentOAuthFailed,
		Message: "The sign-in did not finish. Nothing was saved.",
		Detail:  lastLine(text, f.code),
	}
}

var (
	urlRe   = regexp.MustCompile(`(https?://\S+)\s`)
	tokenRe = regexp.MustCompile(`sk-ant-oat[A-Za-z0-9_-]+`)
	ansiRe  = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][A-Za-z0-9]`)
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

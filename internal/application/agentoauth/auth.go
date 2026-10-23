package agentoauth

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Wrap puts a command under the sandbox driver; it is the orchestration's
// LoginWrap. Nil runs the command as it is.
type Wrap func(ports.SandboxCommand) (ports.SandboxCommand, error)

// statusTimeout bounds one `claude auth status` or `claude auth logout`.
const statusTimeout = 30 * time.Second

// killDelay is how long a killed process may hold its output open.
const killDelay = 2 * time.Second

// AuthDeps are what a claude of the sign-in runs on.
type AuthDeps struct {
	// Bin says which claude to run and whether it can run.
	Bin ports.ManagedClaudeBin
	// ConfigDir is the agent Claude home, passed as CLAUDE_CONFIG_DIR and
	// the directory the process starts in.
	ConfigDir string
	// Env is the environment the process starts from; nil is os.Environ. The
	// variables that would sign it in or move its home are dropped.
	Env func() []string
	// Wrap confines each process; nil runs it unwrapped (a platform without
	// a sandbox driver).
	Wrap Wrap
}

// Auth runs the managed claude's `auth` commands in the agent Claude home,
// under the sandbox driver when there is one: the login and the status share
// one environment and one policy, so the status sees what the login wrote.
type Auth struct{ d AuthDeps }

var _ ports.AgentAuth = (*Auth)(nil)

// NewAuth builds the runner.
func NewAuth(d AuthDeps) *Auth {
	if d.Env == nil {
		d.Env = os.Environ
	}
	return &Auth{d: d}
}

// installed refuses CLAUDE_CLI_NOT_FOUND without a managed claude.
func (a *Auth) installed() error {
	if a.d.Bin == nil || !a.d.Bin.Installed() {
		return &domain.StructuredError{Code: "CLAUDE_CLI_NOT_FOUND", Message: "Sign in needs the managed claude. Install it first."}
	}
	return nil
}

// spec is `claude <args>` as it runs: in the agent home, with its own
// private temp dir (removed by cleanup), wrapped by the driver. The env is
// the base without the person's credentials, then the agent home and a
// browser that opens nothing on the server's machine.
func (a *Auth) spec(args ...string) (spec ports.ProcessSpec, cleanup func(), err error) {
	if err := a.installed(); err != nil {
		return spec, nil, err
	}
	if err := os.MkdirAll(a.d.ConfigDir, 0o700); err != nil {
		return spec, nil, err
	}
	tmp, err := os.MkdirTemp("", "harness-login-")
	if err != nil {
		return spec, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	var env []string
	for _, kv := range a.d.Env() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
			"BROWSER", "TMPDIR", "TMP", "TEMP":
			continue
		}
		env = append(env, kv)
	}
	// The place for the keychain shim of the login (ticket 23): where a
	// platform without a sandbox driver puts it first on PATH.
	env = append(env, "CLAUDE_CONFIG_DIR="+a.d.ConfigDir, "BROWSER=/usr/bin/true",
		"TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	cmd := ports.SandboxCommand{Path: a.d.Bin.Bin(), Args: args, Dir: a.d.ConfigDir, Env: env}
	if a.d.Wrap != nil {
		if cmd, err = a.d.Wrap(cmd); err != nil {
			cleanup()
			return spec, nil, err
		}
	}
	return ports.ProcessSpec{Path: cmd.Path, Args: cmd.Args, Dir: cmd.Dir, Env: cmd.Env}, cleanup, nil
}

// Status runs `claude auth status` and reads its JSON. A signed-out home is
// an answer (LoggedIn false), not an error; claude exits non-zero for it.
func (a *Auth) Status(ctx context.Context) (ports.AgentAuthStatus, error) {
	spec, cleanup, err := a.spec("auth", "status")
	if err != nil {
		return ports.AgentAuthStatus{}, err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = spec.Dir, spec.Env, killDelay
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()
	var got struct {
		LoggedIn         bool   `json:"loggedIn"`
		AuthMethod       string `json:"authMethod"`
		SubscriptionType string `json:"subscriptionType"`
		Email            string `json:"email"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); err != nil {
		if runErr != nil {
			return ports.AgentAuthStatus{}, runErr
		}
		return ports.AgentAuthStatus{}, err
	}
	return ports.AgentAuthStatus{LoggedIn: got.LoggedIn, AuthMethod: got.AuthMethod, SubscriptionType: got.SubscriptionType, Email: got.Email}, nil
}

// Logout runs `claude auth logout`, which removes the login the agent home
// keeps. Its last output line, with any token blotted out, says why it failed.
func (a *Auth) Logout(ctx context.Context) error {
	spec, cleanup, err := a.spec("auth", "logout")
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = spec.Dir, spec.Env, killDelay
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := "Sign out did not finish."
		if detail := lastLine(clean(out.String()), ""); detail != "" {
			msg += " Claude said: " + detail
		} else {
			msg += " " + err.Error()
		}
		return &domain.StructuredError{Code: "AGENT_SIGN_OUT_FAILED", Message: msg}
	}
	return nil
}

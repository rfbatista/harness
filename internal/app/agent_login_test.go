//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// fakeLoginClaude stands in for the managed claude's `auth login --claudeai`
// and `auth status`. It tries to read $LOGIN_SECRET (a stand-in for the
// person's files), keeps the login as .credentials.json in the agent home
// and records what it could do in that home.
const fakeLoginClaude = `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ] && { echo '{"loggedIn": true, "authMethod": "claude.ai", "email": "p@example.com"}'; exit 0; }
  echo '{"loggedIn": false}'
  exit 1
fi
[ "$1 $2 $3" = "auth login --claudeai" ] || exit 2
cat "$LOGIN_SECRET" >/dev/null 2>&1
echo "secret_read=$?" > "$CLAUDE_CONFIG_DIR/probe"
echo "visit: https://claude.com/cai/oauth/authorize?code=true&client_id=fake"
printf "Paste code here if prompted > "
read code
[ "$code" = "good-code" ] || exit 1
echo '{"claudeAiOauth":{}}' > "$CLAUDE_CONFIG_DIR/.credentials.json"
echo "write_login=$?" >> "$CLAUDE_CONFIG_DIR/probe"
`

type loginBin string

func (b loginBin) Bin() string      { return string(b) }
func (b loginBin) Installed() bool  { return true }
func (b loginBin) Overridden() bool { return false }

// runLogin signs in through the real OAuth service with the fake claude
// under driver, using the wiring's loginWrap, and answers the policy the
// driver was asked for (nil for a driver that records nothing).
func runLogin(t *testing.T, driver ports.SandboxDriver) (probe, home string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(fakeLoginClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(t.TempDir(), "agent-home")
	sb := &orchestration.Sandbox{Driver: driver, Home: home, Enabled: func() bool { return false }}
	svc := agentoauth.New(agentoauth.Deps{Bin: loginBin(bin), ConfigDir: home, PTY: ptyunix.New(), Wrap: loginWrap(sb, loginBin(bin))})
	t.Cleanup(svc.Close)
	if _, err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	waitOAuth(t, svc, ports.AgentOAuthWaitingForCode)
	if _, err := svc.Submit("good-code"); err != nil {
		t.Fatal(err)
	}
	waitOAuth(t, svc, ports.AgentOAuthDone)
	b, _ := os.ReadFile(filepath.Join(home, "probe"))
	return string(b), home
}

func waitOAuth(t *testing.T, svc *agentoauth.Service, phase string) {
	t.Helper()
	for end := time.Now().Add(15 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if svc.State().Phase == phase {
			return
		}
	}
	t.Fatalf("flow never reached %q, is at %+v", phase, svc.State())
}

// The login goes to the driver, even with the agent sandbox switched off,
// under a policy of the agent home alone; the env names that home and no
// credential variable of the person.
func TestTheLoginRunsUnderTheDriverWithTheAgentHomeOnlyPolicy(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-person")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-person")
	d := &portstest.RecordingSandboxDriver{}
	_, home := runLogin(t, d)
	calls := d.Calls()
	if len(calls) != 2 {
		t.Fatalf("driver wrapped %d commands, want the login and the status", len(calls))
	}
	if got := strings.Join(calls[0].Command.Args, " "); got != "auth login --claudeai" {
		t.Errorf("login args = %q", got)
	}
	if got := strings.Join(calls[1].Command.Args, " "); got != "auth status" {
		t.Errorf("status args = %q", got)
	}
	userHome, _ := os.UserHomeDir()
	for _, c := range calls {
		if !slices.Contains(c.Policy.Writable, home) {
			t.Errorf("writable = %v, want the agent home", c.Policy.Writable)
		}
		for _, p := range slices.Concat(c.Policy.Readable, c.Policy.Writable) {
			if (userHome != "" && strings.HasPrefix(p, userHome+"/")) || strings.Contains(p, ".ssh") || strings.Contains(p, "Keychains") {
				t.Errorf("policy grants %q to the login", p)
			}
		}
		if !slices.Contains(c.Command.Env, "CLAUDE_CONFIG_DIR="+home) {
			t.Errorf("env lacks the agent home: %v", c.Command.Env)
		}
		for _, kv := range c.Command.Env {
			if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") || strings.HasPrefix(kv, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(kv, "ANTHROPIC_AUTH_TOKEN=") {
				t.Errorf("env carries the person's credential: %s", strings.SplitN(kv, "=", 2)[0])
			}
		}
	}
}

func TestWithoutADriverTheLoginRunsUnwrapped(t *testing.T) {
	if loginWrap(&orchestration.Sandbox{}, loginBin("/x/claude")) != nil || loginWrap(nil, loginBin("/x/claude")) != nil {
		t.Error("a platform without a driver has no wrap")
	}
}

// fakeAuthClaude answers `auth status` and `auth logout` from the login file
// in the agent home, after trying to read $LOGIN_SECRET (a stand-in for the
// person's files) and recording how that went.
const fakeAuthClaude = `#!/bin/sh
cat "$LOGIN_SECRET" >/dev/null 2>&1
echo "$1_$2 secret_read=$?" >> "$CLAUDE_CONFIG_DIR/probe"
if [ "$1 $2" = "auth status" ]; then
  [ -f "$CLAUDE_CONFIG_DIR/.credentials.json" ] && { echo '{"loggedIn": true, "authMethod": "claude.ai", "email": "p@example.com"}'; exit 0; }
  echo '{"loggedIn": false}'
  exit 1
fi
[ "$1 $2" = "auth logout" ] || exit 2
rm -f "$CLAUDE_CONFIG_DIR/.credentials.json"
`

// statusAndLogout reads the status of a signed-in agent home, signs it out
// and reads the status again, through the wiring's loginWrap under driver.
func statusAndLogout(t *testing.T, driver ports.SandboxDriver) (home string, before, after ports.AgentAuthStatus) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(fakeAuthClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(t.TempDir(), "agent-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".credentials.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	sb := &orchestration.Sandbox{Driver: driver, Home: home, Enabled: func() bool { return false }}
	auth := agentoauth.NewAuth(agentoauth.AuthDeps{Bin: loginBin(bin), ConfigDir: home, Wrap: loginWrap(sb, loginBin(bin))})
	ctx := context.Background()
	var err error
	if before, err = auth.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if after, err = auth.Status(ctx); err != nil {
		t.Fatal(err)
	}
	return home, before, after
}

// The status and the logout go to the driver like the login, under the
// agent-home-only policy.
func TestTheStatusAndTheLogoutRunUnderTheDriverWithTheAgentHomeOnlyPolicy(t *testing.T) {
	d := &portstest.RecordingSandboxDriver{}
	home, before, after := statusAndLogout(t, d)
	if !before.LoggedIn || after.LoggedIn {
		t.Errorf("status before = %+v, after = %+v", before, after)
	}
	calls := d.Calls()
	var args []string
	for _, c := range calls {
		args = append(args, strings.Join(c.Command.Args, " "))
		if !slices.Contains(c.Policy.Writable, home) {
			t.Errorf("writable = %v, want the agent home", c.Policy.Writable)
		}
		userHome, _ := os.UserHomeDir()
		for _, p := range slices.Concat(c.Policy.Readable, c.Policy.Writable) {
			if (userHome != "" && strings.HasPrefix(p, userHome+"/")) || strings.Contains(p, ".ssh") || strings.Contains(p, "Keychains") {
				t.Errorf("policy grants %q", p)
			}
		}
	}
	if want := []string{"auth status", "auth logout", "auth status"}; !slices.Equal(args, want) {
		t.Errorf("driver wrapped %q, want %q", args, want)
	}
}

//go:build darwin

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// TestTheLoginUnderTheRealSeatbelt_Darwin runs the stand-in `claude auth
// login` under the host's real sandbox driver with the agent-home-only
// policy: it keeps its login in the agent home and cannot read a file under
// the person's home.
func TestTheLoginUnderTheRealSeatbelt_Darwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	secretDir, err := os.MkdirTemp(home, ".harness-login-secret-")
	if err != nil {
		t.Skip("cannot write under the home directory")
	}
	t.Cleanup(func() { _ = os.RemoveAll(secretDir) })
	secret := filepath.Join(secretDir, "token")
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOGIN_SECRET", secret)

	probe, agentHome := runLogin(t, seatbelt.New())
	if strings.Contains(probe, "secret_read=0") {
		t.Errorf("probe = %q: the stand-in secret under the home was readable", probe)
	}
	if !strings.Contains(probe, "write_login=0") {
		t.Errorf("probe = %q: the login could not be kept in the agent home", probe)
	}
	if _, err := os.Stat(filepath.Join(agentHome, ".credentials.json")); err != nil {
		t.Errorf("no login in the agent home: %v", err)
	}
}

// TestTheRealClaudeLoginPrintsItsURLUnderTheRealSeatbelt_Darwin starts the
// real `claude auth login --claudeai` under Seatbelt in a fresh agent home:
// it prints its authorization URL and waits for the code, then the flow is
// cancelled. It talks to the network, so it runs only with HARNESS_REAL_CLAUDE
// set.
func TestTheRealClaudeLoginPrintsItsURLUnderTheRealSeatbelt_Darwin(t *testing.T) {
	if os.Getenv("HARNESS_REAL_CLAUDE") == "" {
		t.Skip("HARNESS_REAL_CLAUDE is not set")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude is not installed")
	}
	home := filepath.Join(t.TempDir(), "agent-home")
	sb := &orchestration.Sandbox{Driver: seatbelt.New(), Home: home}
	svc := agentoauth.New(agentoauth.Deps{Bin: loginBin(bin), ConfigDir: home, PTY: ptyunix.New(), Wrap: loginWrap(sb, loginBin(bin))})
	t.Cleanup(svc.Close)
	if _, err := svc.Start(); err != nil {
		t.Fatal(err)
	}
	waitOAuth(t, svc, ports.AgentOAuthWaitingForCode)
	if url := svc.State().URL; !strings.HasPrefix(url, "https://") || !strings.Contains(url, "oauth/authorize") {
		t.Errorf("url = %q", url)
	}
	svc.Cancel()
	if _, err := os.Stat(filepath.Join(home, ".credentials.json")); err == nil {
		t.Error("a cancelled login kept a credential")
	}
}

// TestTheStatusAndTheLogoutUnderTheRealSeatbelt_Darwin runs the stand-in
// `claude auth status` and `claude auth logout` under the host's real sandbox
// driver: they see and remove the login in the agent home and cannot read a
// file under the person's home.
func TestTheStatusAndTheLogoutUnderTheRealSeatbelt_Darwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	secretDir, err := os.MkdirTemp(home, ".harness-login-secret-")
	if err != nil {
		t.Skip("cannot write under the home directory")
	}
	t.Cleanup(func() { _ = os.RemoveAll(secretDir) })
	secret := filepath.Join(secretDir, "token")
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOGIN_SECRET", secret)

	agentHome, before, after := statusAndLogout(t, seatbelt.New())
	if !before.LoggedIn || before.Email != "p@example.com" {
		t.Errorf("status before logout = %+v", before)
	}
	if after.LoggedIn {
		t.Errorf("status after logout = %+v, want signed out", after)
	}
	if _, err := os.Stat(filepath.Join(agentHome, ".credentials.json")); err == nil {
		t.Error("logout under the sandbox left the login")
	}
	probe, _ := os.ReadFile(filepath.Join(agentHome, "probe"))
	if !strings.Contains(string(probe), "auth_status secret_read=1") || !strings.Contains(string(probe), "auth_logout secret_read=1") || strings.Contains(string(probe), "secret_read=0") {
		t.Errorf("probe = %q: the stand-in secret was readable, or a command did not run", probe)
	}
}

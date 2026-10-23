//go:build darwin

package shim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain makes the test binary the `security` shim when it is run under
// that name, logging each call to $SECURITY_SHIM_LOG first, so a real claude
// can be watched talking to it.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "security" {
		code := securityMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, realSecurity)
		if f, err := os.OpenFile(os.Getenv("SECURITY_SHIM_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			// Never the secret: the command and its service only.
			cmd := ""
			if len(os.Args) > 1 {
				cmd = os.Args[1]
			}
			_, _ = f.WriteString(cmd + " " + serviceOf(os.Args[1:]) + "\n")
			_ = f.Close()
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// A real claude with the shim first on PATH and a login file in its config
// dir reads the file, removes it on logout, asks the shim for the item and
// never writes one, and no keychain item of that config dir exists after.
func TestARealClaudeUnderTheShimKeepsItsLoginInTheFileAndWritesNoKeychainItem(t *testing.T) {
	claude, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("no claude on PATH")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shims := t.TempDir()
	if err := os.Symlink(self, filepath.Join(shims, "security")); err != nil {
		t.Fatal(err)
	}
	// Resolve symlinks: claude hashes the path it is given, and macOS's
	// temp dir is itself behind a symlink.
	cfg, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	login := filepath.Join(cfg, ".credentials.json")
	fake := `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-fake","refreshToken":"sk-ant-ort01-fake","expiresAt":4102444800000,"scopes":["user:inference","user:profile"],"subscriptionType":"max"}}`
	if err := os.WriteFile(login, []byte(fake), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	var env []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name != "ANTHROPIC_API_KEY" && name != "CLAUDE_CODE_OAUTH_TOKEN" && name != "ANTHROPIC_AUTH_TOKEN" &&
			name != "CLAUDE_CONFIG_DIR" && name != "PATH" {
			env = append(env, kv)
		}
	}
	env = append(env, "CLAUDE_CONFIG_DIR="+cfg, "PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"), "SECURITY_SHIM_LOG="+log)
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, claude, args...)
		cmd.Env, cmd.Dir = env, cfg
		out, _ := cmd.CombinedOutput() // a signed-out status exits non-zero
		return string(out)
	}

	if out := run("auth", "status"); !strings.Contains(out, `"loggedIn": true`) {
		t.Fatalf("auth status did not read the login file: %s", out)
	}
	run("auth", "logout")
	if _, err := os.Stat(login); !os.IsNotExist(err) {
		t.Errorf("auth logout left the login file: %v", err)
	}

	calls, _ := os.ReadFile(log)
	got := string(calls)
	if !strings.Contains(got, "find-generic-password Claude Code-credentials") || !strings.Contains(got, "delete-generic-password") {
		t.Errorf("the shim saw %q, want a find of the credentials item and a delete", got)
	}
	if strings.Contains(got, "add-generic-password") || strings.Contains(got, "-i ") {
		t.Errorf("the shim saw an add: %q", got)
	}

	// Metadata only, never -w: no item of this config dir's hashed name.
	sum := sha256.Sum256([]byte(cfg))
	service := "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
	if err := exec.Command(realSecurity, "find-generic-password", "-s", service).Run(); err == nil {
		t.Errorf("a keychain item %q exists", service)
	} else if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 44 {
		t.Errorf("security find for %q: %v, want exit 44", service, err)
	}
}

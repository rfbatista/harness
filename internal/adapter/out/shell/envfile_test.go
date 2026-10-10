package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// A person's profile may export the machine's agent socket or GitHub token.
// A login shell sources the command's env file after it, so the session's
// git credentials win.
func TestALoginShellSourcesTheEnvFileAfterTheProfile(t *testing.T) {
	home := t.TempDir()
	profile := "export GITHUB_TOKEN=machine-token\nexport SSH_AUTH_SOCK=/machine/agent\n"
	for _, name := range []string{".profile", ".bash_profile", ".zprofile"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(profile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	envFile := filepath.Join(t.TempDir(), "env.sh")
	if err := os.WriteFile(envFile, []byte("export GITHUB_TOKEN='stored-token'\nunset SSH_AUTH_SOCK\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(envFile string) string {
		t.Helper()
		spec, err := Login{Shell: "/bin/sh"}.Prepare(context.Background(), ports.ShellCommand{
			Program: "/bin/sh", Args: []string{"-c", `printf '%s|%s' "$GITHUB_TOKEN" "${SSH_AUTH_SOCK-unset}"`},
			Env: []string{"HOME=" + home}, EnvFile: envFile,
		})
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(spec.Path, spec.Args...)
		cmd.Env = spec.Env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("%v: %v", spec.Args, err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run(""); got != "machine-token|/machine/agent" {
		t.Fatalf("without an env file the profile's values should show (the test proves nothing otherwise): %q", got)
	}
	if got := run(envFile); got != "stored-token|unset" {
		t.Fatalf("with the env file: %q, want the session's values", got)
	}
}

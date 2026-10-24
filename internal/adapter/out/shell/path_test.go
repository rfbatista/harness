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

// A login shell whose profile resets PATH puts the session's dirs back in
// order: the shims, the token savers', the keychain shim, the developer bin,
// then what the profile left.
func TestALoginShellPutsTheSessionsPathDirsBackInOrderAfterTheProfile(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "login-sh")
	script := "#!/bin/sh\n[ \"$1\" = -l ] && shift\nPATH=/usr/bin:/bin; export PATH\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(profile, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) string {
		t.Helper()
		spec, err := Login{Shell: profile}.Prepare(context.Background(), ports.ShellCommand{
			Program: "/bin/sh", Args: []string{"-c", `printf %s "$PATH"`}, Env: env,
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
	got := run("PATH=/ignored", ports.ShimDirEnv+"=/shims", ports.SaverBinDirsEnv+"=/rtk:/other",
		ports.KeychainShimDirEnv+"=/kc", ports.DeveloperBinEnv+"=/dev")
	if want := "/shims:/rtk:/other:/kc:/dev:/usr/bin:/bin"; got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
	if got := run("PATH=/ignored", ports.SaverBinDirsEnv+"=/rtk"); got != "/rtk:/usr/bin:/bin" {
		t.Errorf("PATH with only a saver = %q", got)
	}
	if got := run("PATH=/ignored"); got != "/usr/bin:/bin" {
		t.Errorf("PATH with nothing to restore = %q", got)
	}
}

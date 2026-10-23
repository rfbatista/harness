//go:build darwin

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/adapter/out/seatbelt"
	"operators-mcp/internal/application/orchestration"
)

// fakeConfinedOneShot writes into its cwd, tries to read $ONESHOT_SECRET and
// answers what happened as its result.
const fakeConfinedOneShot = `#!/bin/sh
echo x > "$(pwd)/oneshot-probe-$$" 2>/dev/null
w=$?
rm -f "$(pwd)/oneshot-probe-$$"
cat "$ONESHOT_SECRET" >/dev/null 2>&1
r=$?
printf '{"subtype":"success","is_error":false,"result":"write=%s read=%s"}' "$w" "$r"
`

// TestOneShot_RunsUnderTheRealSeatbeltPolicy_Darwin runs a stand-in one-shot
// claude under the host's real sandbox driver: it can write its temp cwd and
// cannot read a stand-in secret under the person's home.
func TestOneShot_RunsUnderTheRealSeatbeltPolicy_Darwin(t *testing.T) {
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec is not available")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	secretDir, err := os.MkdirTemp(home, ".harness-oneshot-secret-")
	if err != nil {
		t.Skip("cannot write under the home directory")
	}
	t.Cleanup(func() { _ = os.RemoveAll(secretDir) })
	secret := filepath.Join(secretDir, "token")
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONESHOT_SECRET", secret)

	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(fakeConfinedOneShot), 0o755); err != nil {
		t.Fatal(err)
	}
	agentHome := t.TempDir()
	sb := &orchestration.Sandbox{Driver: seatbelt.New(), Home: agentHome}
	one := claudetext.Confined(oneShotClaude(bin, agentHome, ""), oneShotPrep(sb, func() []string { return nil }, bin))

	got, err := claudetext.NewSummarizer(one, bin, "").Summarize(context.Background(), "text")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "write=0") {
		t.Errorf("result = %q, want the temp cwd writable", got)
	}
	if strings.Contains(got, "read=0") {
		t.Errorf("result = %q, the stand-in secret under the home was readable", got)
	}
}

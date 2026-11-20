package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports/portstest"
)

// A project's one-shots run on the project's effective credential (ADR 0102),
// like its sessions: its login means its Claude home as the config dir; its
// pasted credential, its own env; a call with no project keeps the global
// agent credential.
func TestAProjectsOneShotRunsOnItsEffectiveCredential(t *testing.T) {
	for name, tc := range map[string]struct {
		project       string
		wantConfig    string // "agent", "project"
		wantToken     string
		wantWritable  bool // the project Claude home is writable in the policy
		otherReadable bool
	}{
		"project login":    {"login", "project", "", true, false},
		"project pasted":   {"pasted", "agent", "tok-project", false, false},
		"project, nothing": {"plain", "agent", "tok-global", false, false},
		"no project":       {"", "agent", "tok-global", false, false},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "claude")
			if err := os.WriteFile(bin, []byte(fakeOneShotClaude), 0o755); err != nil {
				t.Fatal(err)
			}
			agentHome := t.TempDir()
			homes := t.TempDir()
			for _, id := range []string{"login", "other"} {
				if err := os.MkdirAll(filepath.Join(homes, id), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("TMPDIR", t.TempDir())
			driver := &portstest.RecordingSandboxDriver{}
			sb := &orchestration.Sandbox{Driver: driver, Home: agentHome, ProjectHomesRoot: homes}
			src := oneShotSources{
				Pasted:     func() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-global"} },
				AgentLogin: func() bool { return false },
				ProjectLoginHome: func(id string) string {
					if id == "login" {
						return filepath.Join(homes, id)
					}
					return ""
				},
				ProjectCredential: func(id string) ([]string, bool) {
					if id == "pasted" {
						return []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-project"}, true
					}
					return nil, false
				},
			}
			one := claudetext.ConfinedFor(oneShotClaude(bin, agentHome, ""), oneShotPrepFor(sb, src, bin))
			ctx := claudetext.WithProject(context.Background(), tc.project)
			if _, err := one.OneShot(ctx, claude.OneShotRequest{System: "s", Input: "i"}); err != nil {
				t.Fatal(err)
			}
			rec, _ := os.ReadFile(filepath.Join(dir, "record"))
			wantDir := agentHome
			if tc.wantConfig == "project" {
				wantDir = filepath.Join(homes, "login")
			}
			if !strings.Contains(string(rec), "config="+wantDir+"\n") {
				t.Errorf("the claude saw %q, want config dir %q", rec, wantDir)
			}
			if !strings.Contains(string(rec), "token="+tc.wantToken+"\n") {
				t.Errorf("the claude saw %q, want token %q", rec, tc.wantToken)
			}
			calls := driver.Calls()
			if len(calls) != 1 {
				t.Fatalf("driver called %d times", len(calls))
			}
			pol := calls[0].Policy
			if got := slices.Contains(pol.Writable, filepath.Join(homes, "login")); got != tc.wantWritable {
				t.Errorf("project Claude home writable = %v, want %v: %+v", got, tc.wantWritable, pol)
			}
			for _, list := range [][]string{pol.Readable, pol.Writable} {
				if slices.Contains(list, filepath.Join(homes, "other")) || slices.Contains(list, homes) {
					t.Errorf("another project's home is reachable: %v", list)
				}
			}
		})
	}
}

package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/agents/claudetext"
	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

type memGlobalStore struct{ rec *ports.AgentCredentialRecord }

func (m *memGlobalStore) Get() (*ports.AgentCredentialRecord, error) { return m.rec, nil }
func (m *memGlobalStore) Put(r ports.AgentCredentialRecord) error    { m.rec = &r; return nil }
func (m *memGlobalStore) Delete() error                              { m.rec = nil; return nil }

type memProjectStore map[string]ports.AgentCredentialRecord

func (m memProjectStore) GetFor(id string) (*ports.AgentCredentialRecord, error) {
	r, ok := m[id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m memProjectStore) PutFor(id string, r ports.AgentCredentialRecord) error {
	m[id] = r
	return nil
}
func (m memProjectStore) DeleteFor(id string) error { delete(m, id); return nil }

// xorBox is a stand-in cipher: enough to tell sealed from plain.
type xorBox struct{}

func (xorBox) KeyID() string { return "k" }
func (xorBox) Seal(p []byte, _ bool) ([]byte, string, error) {
	out := slices.Clone(p)
	for i := range out {
		out[i] ^= 0x55
	}
	return out, "k", nil
}
func (xorBox) Open(s []byte) ([]byte, error) {
	out := slices.Clone(s)
	for i := range out {
		out[i] ^= 0x55
	}
	return out, nil
}

// The architecture assistant's read-only turn runs on its project's effective
// credential like a session does (ADR 0102): a project login means its Claude
// home as the config dir and no credential env; a project pasted credential
// means its own env on the shared home; neither falls back to the global rule.
// Every turn is still confined by the deny-writes wrap, and no turn is granted
// another project's Claude home.
func TestTheAssistantsTurnRunsOnItsProjectsEffectiveCredential(t *testing.T) {
	cases := map[string]struct {
		project    string
		wantConfig string // "" for the agent home (the manager's)
		wantEnv    []string
	}{
		"project pasted credential": {"pasted", "", []string{"ANTHROPIC_API_KEY=sk-ant-project"}},
		"project login":             {"login", "HOME:login", nil},
		"project with nothing":      {"plain", "", []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-global"}},
		"no project":                {"", "", []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-global"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			homes := t.TempDir()
			t.Setenv("TMPDIR", t.TempDir())
			for _, id := range []string{"login", "other"} {
				if err := os.MkdirAll(filepath.Join(homes, id), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			cred := agentcredential.New(&memGlobalStore{}, xorBox{}, func() time.Time { return time.Unix(0, 0) }).WithProjects(memProjectStore{})
			if _, err := cred.Set(ports.AgentCredentialOAuth, "tok-global"); err != nil {
				t.Fatal(err)
			}
			if _, err := cred.SetFor("pasted", ports.AgentCredentialAPIKey, "sk-ant-project"); err != nil {
				t.Fatal(err)
			}
			driver := &portstest.RecordingSandboxDriver{}
			orch := &orchestration.Service{
				Sandbox: &orchestration.Sandbox{Driver: driver, Home: t.TempDir(), ProjectHomesRoot: homes},
				ProjectLoginHome: func(id string) string {
					if id == "login" {
						return filepath.Join(homes, id)
					}
					return ""
				},
			}
			p := oneShotParts{Orch: orch, Credential: cred, Home: claudehome.NewHome(t.TempDir())}
			rec := &recordingTurn{}
			turn := assistantTurn{claudetext.ConfinedTurnFor(rec, readOnlyTurnPrep("/bin/claude", p))}
			_, err := turn.Run(context.Background(), archassistant.TurnCall{Dir: t.TempDir(), ProjectID: tc.project}, archassistant.TurnCallbacks{OnText: func(string) {}, OnTool: func(string, string) {}})
			if err != nil {
				t.Fatal(err)
			}

			wantConfig := strings.Replace(tc.wantConfig, "HOME:", homes+"/", 1)
			if rec.got.ConfigDir != wantConfig {
				t.Errorf("config dir %q, want %q", rec.got.ConfigDir, wantConfig)
			}
			var got []string
			for _, e := range rec.got.Env {
				if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
					got = append(got, e)
				}
			}
			if !slices.Equal(got, tc.wantEnv) {
				t.Errorf("credential env %v, want %v", got, tc.wantEnv)
			}
			if rec.got.Wrap == nil {
				t.Error("the turn is not wrapped by the sandbox")
			}
			// The wrap builds the policy when it runs a command.
			if _, err := rec.got.Wrap(llmkit.Command{Path: "/bin/claude", Dir: rec.got.Dir, Env: rec.got.Env}); err != nil {
				t.Fatal(err)
			}
			calls := driver.Calls()
			if len(calls) != 1 {
				t.Fatalf("driver called %d times, want 1", len(calls))
			}
			if own := slices.Contains(calls[0].Policy.Writable, filepath.Join(homes, "login")); own != (tc.project == "login") {
				t.Errorf("own project Claude home writable = %v for project %q", own, tc.project)
			}
			for _, list := range [][]string{calls[0].Policy.Readable, calls[0].Policy.Writable} {
				if slices.Contains(list, filepath.Join(homes, "other")) || slices.Contains(list, homes) {
					t.Errorf("another project's Claude home is reachable: %v", list)
				}
			}
		})
	}
}

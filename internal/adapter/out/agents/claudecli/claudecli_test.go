package claudecli

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/ports"
)

// errCaptured stops a start once the command is known.
var errCaptured = errors.New("captured")

// commandOf is the command llmkit would start for spec, as the shell got it.
func commandOf(t *testing.T, spec ports.AgentSpec, proxyURL string) (ports.ShellCommand, error) {
	t.Helper()
	sh := &capturingShell{}
	_, err := NewLauncher(claude.New(llmkit.Options{Bin: "/opt/claude", ProxyURL: proxyURL})).Launch(context.Background(), spec, ports.TermSize{Cols: 80, Rows: 24}, sh)
	if errors.Is(err, errCaptured) {
		return sh.got, nil
	}
	return ports.ShellCommand{}, err
}

type capturingShell struct{ got ports.ShellCommand }

func (s *capturingShell) Prepare(_ context.Context, cmd ports.ShellCommand) (ports.ProcessSpec, error) {
	s.got = cmd
	return ports.ProcessSpec{}, errCaptured
}

func (s *capturingShell) CheckDir(context.Context, string) error { return nil }

func envOf(env []string, key string) string {
	var v string
	for _, kv := range env {
		if val, ok := strings.CutPrefix(kv, key+"="); ok {
			v = val
		}
	}
	return v
}

func TestTheLauncherStartsClaudeThroughLlmkitInTheShell(t *testing.T) {
	spec := ports.AgentSpec{
		Kind: Kind, SessionID: "s1", Dir: "/work", Env: []string{"A=1"},
		Permission: "bypass", SkillDirs: []string{"/skills"},
		Conversation: ports.Conversation{ID: "c1", Resume: true},
		EnvFile:      "/data/git/sessions/c/env.sh",
	}
	cmd, err := commandOf(t, spec, "http://127.0.0.1:8080/llm")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Program != "/opt/claude" || cmd.Dir != "/work" || envOf(cmd.Env, "A") != "1" || cmd.EnvFile != "/data/git/sessions/c/env.sh" {
		t.Fatalf("command = %+v, want claude in /work with the spec's env and env file", cmd)
	}
	want := []string{"--resume", "c1", "--permission-mode", "bypassPermissions", "--plugin-dir", "/skills"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}
	if envOf(cmd.Env, "ANTHROPIC_BASE_URL") != "http://127.0.0.1:8080/llm" || !strings.Contains(envOf(cmd.Env, "ANTHROPIC_CUSTOM_HEADERS"), "X-Llm-Attribution: s1") {
		t.Errorf("env routes through %q with headers %q, want the proxy and the session's id as attribution",
			envOf(cmd.Env, "ANTHROPIC_BASE_URL"), envOf(cmd.Env, "ANTHROPIC_CUSTOM_HEADERS"))
	}
}

func TestWithoutAProxyClaudeCallsTheAPIDirectly(t *testing.T) {
	cmd, err := commandOf(t, ports.AgentSpec{SessionID: "s1", Conversation: ports.Conversation{ID: "c"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if v := envOf(cmd.Env, "ANTHROPIC_CUSTOM_HEADERS"); strings.Contains(v, "X-Llm-Attribution") {
		t.Errorf("headers = %q, want no attribution without a proxy", v)
	}
}

func TestTheLauncherRejects(t *testing.T) {
	if _, err := commandOf(t, ports.AgentSpec{Kind: "codex", Conversation: ports.Conversation{ID: "c"}}, ""); err == nil {
		t.Error("ran a spec for another agent")
	}
	if _, err := commandOf(t, ports.AgentSpec{Kind: Kind}, ""); err == nil {
		t.Error("ran a spec with no conversation id")
	}
}

// The hooks report to the server with their name as event=; only Stop's
// answer reaches claude, since it carries the turns that waited.
func TestTheSessionHooksReportToTheServer(t *testing.T) {
	cmd, err := commandOf(t, ports.AgentSpec{Conversation: ports.Conversation{ID: "c1"}, HookURL: "http://127.0.0.1:8080/api/interactive_session_hook?session_id=s1",
		Hooks: map[string][]ports.HookGroup{"PreToolUse": {{Matcher: "Edit", Hooks: []ports.HookCommand{{Type: "command", Command: "edit-proxy"}}}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(cmd.Args, "--settings")
	if i < 0 {
		t.Fatalf("no --settings in %v", cmd.Args)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string                     `json:"matcher"`
			Hooks   []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(cmd.Args[i+1]), &settings); err != nil {
		t.Fatal(err)
	}
	for event, discards := range map[string]bool{"SessionStart": true, "UserPromptSubmit": true, "Stop": false} {
		c := settings.Hooks[event][0].Hooks[0].Command
		if !strings.Contains(c, "session_id=s1&event="+event+"'") {
			t.Errorf("%s posts to the wrong url: %s", event, c)
		}
		if strings.Contains(c, "' >/dev/null 2>&1") != discards {
			t.Errorf("%s stdout discarded = %v, want %v: %s", event, !discards, discards, c)
		}
		if !strings.HasSuffix(c, "|| true") {
			t.Errorf("%s must never fail the session: %s", event, c)
		}
	}
	if pre := settings.Hooks["PreToolUse"]; len(pre) != 1 || pre[0].Matcher != "Edit" || pre[0].Hooks[0].Command != "edit-proxy" {
		t.Errorf("PreToolUse hooks = %+v, want the spec's edit proxy", pre)
	}
}

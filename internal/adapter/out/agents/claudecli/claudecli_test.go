package claudecli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

func TestCommand(t *testing.T) {
	spec := ports.AgentSpec{
		Kind: Kind, SessionID: "s1", Dir: "/work", Env: []string{"A=1"},
		Permission: "bypass", SkillDirs: []string{"/skills"},
		Conversation: ports.Conversation{ID: "c1", Resume: true},
	}
	cmd, err := New("/opt/claude").Command(spec)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Program != "/opt/claude" || cmd.Dir != "/work" || !slices.Equal(cmd.Env, []string{"A=1"}) {
		t.Fatalf("command = %+v", cmd)
	}
	want := []string{"--resume", "c1", "--permission-mode", "bypassPermissions", "--plugin-dir", "/skills"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}

	if cmd, _ := New("").Command(ports.AgentSpec{Conversation: ports.Conversation{ID: "c"}}); cmd.Program != "claude" {
		t.Errorf("default program = %q, want claude", cmd.Program)
	}
}

func TestCommandRejects(t *testing.T) {
	if _, err := New("").Command(ports.AgentSpec{Kind: "codex", Conversation: ports.Conversation{ID: "c"}}); err == nil {
		t.Error("ran a spec for another agent")
	}
	if _, err := New("").Command(ports.AgentSpec{Kind: Kind}); err == nil {
		t.Error("ran a spec with no conversation id")
	}
}

// The hooks report to the server with their name as event=; only Stop's
// answer reaches claude, since it carries the turns that waited.
func TestCommand_SessionHooks(t *testing.T) {
	cmd, err := New("").Command(ports.AgentSpec{Conversation: ports.Conversation{ID: "c1"}, HookURL: "http://127.0.0.1:8080/api/interactive_session_hook?session_id=s1"})
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(cmd.Args, "--settings")
	if i < 0 {
		t.Fatalf("no --settings in %v", cmd.Args)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
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
}

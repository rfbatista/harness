package claudecli

import (
	"slices"
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

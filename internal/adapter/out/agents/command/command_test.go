package command

import (
	"slices"
	"testing"

	"operators-mcp/internal/ports"
)

func TestCommandRunsTheLineThroughALoginShell(t *testing.T) {
	cmd, err := Agent{Shell: "/bin/zsh"}.Command(ports.AgentSpec{Kind: Kind, Command: " make air ", Dir: "/w", Env: []string{"A=1"}})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Program != "/bin/zsh" || !slices.Equal(cmd.Args, []string{"-l", "-c", "make air"}) || cmd.Dir != "/w" || !slices.Equal(cmd.Env, []string{"A=1"}) {
		t.Fatalf("got %+v", cmd)
	}
	if _, err := New().Command(ports.AgentSpec{Kind: Kind}); err == nil {
		t.Fatal("an empty command must be refused")
	}
}

func TestCommandSourcesTheEnvFileFirst(t *testing.T) {
	cmd, err := Agent{Shell: "/bin/sh"}.Command(ports.AgentSpec{Kind: Kind, Command: "make air", EnvFile: "/data/git/sessions/s'1/env.sh"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-l", "-c", `. '/data/git/sessions/s'\''1/env.sh'; make air`}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("args %q, want %q", cmd.Args, want)
	}
}

// Package command is the ports.Agent that runs a plain shell command line in
// a terminal, such as a repository's `make air`: the "command" kind the
// application runs use. It runs through the user's shell as a login shell,
// so the PATH their profile sets (nvm, asdf, …) applies.
package command

import (
	"os"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Kind is the AgentSpec.Kind this agent serves.
const Kind = "command"

var _ ports.Agent = Agent{}

// Agent runs AgentSpec.Command with Shell (empty: $SHELL, else /bin/sh).
type Agent struct{ Shell string }

// New returns the agent.
func New() Agent { return Agent{} }

func (Agent) Kind() string { return Kind }

// Command is `<shell> -l -c <command>` in the spec's directory.
func (a Agent) Command(spec ports.AgentSpec) (ports.ShellCommand, error) {
	line := strings.TrimSpace(spec.Command)
	if line == "" {
		return ports.ShellCommand{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "no command to run"}
	}
	sh := a.Shell
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		sh = "/bin/sh"
	}
	return ports.ShellCommand{
		Program: sh,
		Args:    []string{"-l", "-c", line},
		Dir:     spec.Dir,
		Env:     spec.Env,
	}, nil
}

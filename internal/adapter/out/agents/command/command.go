// Package command is the ports.Agent that runs a plain shell command line in
// a terminal, such as a repository's `make air`: the "command" kind the
// application runs use. It runs through the user's shell as a login shell,
// so the PATH their profile sets (nvm, asdf, …) applies.
package command

import (
	"os"
	"slices"
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

// Command is `<shell> -l -c <command>` in the spec's directory, sourcing
// the spec's env file first when it has one.
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
	if spec.EnvFile != "" {
		// After the profile, so it cannot override the session's git
		// credentials.
		line = ". " + shellQuote(spec.EnvFile) + "; " + line
	}
	env := spec.Env
	if len(spec.Dotenv) > 0 {
		var exports string
		exports, env = dotenvExports(spec.Dotenv, spec.Env)
		line = exports + line
	}
	return ports.ShellCommand{
		Program: sh,
		Args:    []string{"-l", "-c", line},
		Dir:     spec.Dir,
		Env:     env,
	}, nil
}

// dotenvPrefix carries a .env variable through the login profile, which
// would override it under its own name.
const dotenvPrefix = "HARNESS_DOTENV_"

// dotenvExports puts the .env's values in the environment under
// dotenvPrefix and returns the line that moves them to their own names once
// the profile has run. The line is the harness's own: it names keys
// (validated, so they are plain words) and expands each value once inside
// double quotes, where the shell does no further substitution, so a $(…) or
// a backtick in a value stays text. No value is ever written into a script,
// a file or the command line.
func dotenvExports(vars []domain.EnvVar, env []string) (string, []string) {
	env = slices.Clone(env)
	var b strings.Builder
	for _, v := range vars {
		if !domain.ValidEnvKey(v.Key) {
			continue
		}
		carrier := dotenvPrefix + v.Key
		env = append(env, carrier+"="+v.Value)
		b.WriteString("export " + v.Key + `="$` + carrier + `"; unset ` + carrier + "; ")
	}
	return b.String(), env
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

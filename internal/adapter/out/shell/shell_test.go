//go:build !windows

package shell

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/runtimetest"
)

func TestDirectConformance(t *testing.T) {
	runtimetest.ShellConformance(t, func(*testing.T) ports.Shell { return Direct{} })
}

func TestLoginConformance(t *testing.T) {
	runtimetest.ShellConformance(t, func(*testing.T) ports.Shell { return Login{Shell: "/bin/sh"} })
}

func TestDirectReportsAMissingProgram(t *testing.T) {
	_, err := Direct{}.Prepare(context.Background(), ports.ShellCommand{Program: "no-such-agent-cli"})
	if got := errs.Code(err); got != "AGENT_CLI_NOT_FOUND" {
		t.Fatalf("code = %q, want AGENT_CLI_NOT_FOUND", got)
	}
}

func TestASessionsCommandNeverInheritsThePersonCredentialWhateverTheClientsEnvironmentHolds(t *testing.T) {
	t.Setenv("CODING_POOL_PERSON", "the-credential")
	t.Setenv("CODING_POOL_PERSON_NOT", "kept")
	for _, kv := range env(ports.ShellCommand{Env: []string{"A=b"}}) {
		if strings.HasPrefix(kv, "CODING_POOL_PERSON=") {
			t.Errorf("the command's environment holds %q", kv)
		}
	}
	got := env(ports.ShellCommand{Env: []string{"A=b"}})
	if !slices.Contains(got, "A=b") || !slices.Contains(got, "CODING_POOL_PERSON_NOT=kept") {
		t.Errorf("env dropped more than the credential: %v", got)
	}
}

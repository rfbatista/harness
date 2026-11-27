//go:build !windows

package shell

import (
	"context"
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

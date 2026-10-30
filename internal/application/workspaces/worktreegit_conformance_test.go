package workspaces_test

import (
	"testing"

	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestWorktreeGitConformance(t *testing.T) {
	portstest.WorktreeGitConformance(t, func(t *testing.T, f *portstest.WorktreeGitFixture) (ports.WorktreeGit, portstest.WorktreeGitTarget) {
		return workspacestest.NewWorktreeGit(t, f)
	})
}

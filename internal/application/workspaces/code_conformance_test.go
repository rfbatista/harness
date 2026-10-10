package workspaces_test

import (
	"testing"

	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestCodeConformance(t *testing.T) {
	portstest.CodeConformance(t, func(t *testing.T, f *portstest.CodeFixture) (ports.CodeBrowser, portstest.CodeTarget) {
		return workspacestest.NewCode(t, f)
	})
}

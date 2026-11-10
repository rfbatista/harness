package gitcli

import (
	"testing"

	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestGitBranchEvidenceConformance(t *testing.T) {
	portstest.GitBranchEvidenceConformance(t, func() ports.GitBranchEvidence { return NewCode() })
}

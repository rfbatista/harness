package artifacts_test

import (
	"testing"

	"operators-mcp/internal/application/artifacts/artifactstest"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestArtifactAttachmentsConformance(t *testing.T) {
	portstest.ArtifactAttachmentsConformance(t, func(t *testing.T) (ports.ArtifactAttachments, portstest.AttachmentFixture) {
		return artifactstest.New(t)
	})
}

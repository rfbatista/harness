package httpclient

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ArtifactAttachments = (*Artifacts)(nil)

// Artifacts is ports.ArtifactAttachments over the HTTP API.
type Artifacts struct{ c *Client }

// NewArtifacts returns the artifacts adapter over c.
func NewArtifacts(c *Client) *Artifacts { return &Artifacts{c: c} }

func (a *Artifacts) AttachArtifactToTicket(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
	return a.link(ctx, "/api/attach_artifact_to_ticket", artifactID, ticketID)
}

func (a *Artifacts) DetachArtifactFromTicket(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
	return a.link(ctx, "/api/detach_artifact_from_ticket", artifactID, ticketID)
}

func (a *Artifacts) link(ctx context.Context, path, artifactID, ticketID string) (*domain.Artifact, error) {
	var out struct {
		Artifact *domain.Artifact `json:"artifact"`
	}
	err := a.c.post(ctx, path, map[string]string{"artifact_id": artifactID, "ticket_id": ticketID}, &out)
	return out.Artifact, err
}

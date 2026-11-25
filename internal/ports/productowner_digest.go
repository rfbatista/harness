package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// ProductOwnerDigestStore keeps each project's digests and where they stand
// (ADR 0113).
type ProductOwnerDigestStore interface {
	SaveDigest(ctx context.Context, d domain.ProductOwnerDigest) error
	// Digests are the project's, newest first: at most limit, dated before
	// the day before ("" for the newest). hasEarlier says older ones exist.
	Digests(ctx context.Context, projectID, before string, limit int) (digests []domain.ProductOwnerDigest, hasEarlier bool, err error)
	// DigestState is nil while the project has none.
	DigestState(ctx context.Context, projectID string) (*domain.ProductOwnerDigestState, error)
	SaveDigestState(ctx context.Context, s domain.ProductOwnerDigestState) error
}

// ProductOwnerDigests is what the web UI's API reads of a project's digests.
type ProductOwnerDigests interface {
	// Digests answers PROJECT_NOT_FOUND for an unknown project.
	Digests(ctx context.Context, projectID, before string) (digests []domain.ProductOwnerDigest, hasEarlier bool, err error)
}

// ProductOwnerDigestChange is a digest recorded.
type ProductOwnerDigestChange struct {
	ProjectID string `json:"project_id"`
	DigestID  string `json:"digest_id"`
	Date      string `json:"date"`
}

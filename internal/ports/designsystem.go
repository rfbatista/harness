package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// DesignSystems is the design system use cases: one structured record per
// project, written only by the person here, kept as immutable versions.
// *designsystem.Service satisfies it.
//
// An unknown project is PROJECT_NOT_FOUND.
type DesignSystems interface {
	// GetDesignSystem is the latest version's record, or the empty record
	// (Exists false, version 0) when none was saved. It stores nothing.
	GetDesignSystem(ctx context.Context, projectID string) (*domain.DesignSystem, error)
	// SaveDesignSystem writes record as the next version (source manual)
	// when baseVersion is the current version (0 for the first save).
	// DESIGN_SYSTEM_INVALID carries the field errors; DESIGN_SYSTEM_CHANGED
	// says the current version moved.
	SaveDesignSystem(ctx context.Context, projectID string, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error)
	// RenameDesignToken renames a token and rewrites the previews that use it
	// as one version, with the same refusals as a save.
	RenameDesignToken(ctx context.Context, projectID string, baseVersion int, group, name, newGroup, newName string) (*domain.DesignSystemVersion, error)
	// ListDesignSystemVersions lists versions newest first, records included
	// (callers that list drop them): at most limit of them (a default when 0),
	// older than before (every one when 0).
	ListDesignSystemVersions(ctx context.Context, projectID string, limit, before int) ([]*domain.DesignSystemVersion, error)
	// GetDesignSystemVersion is one version with its record; VERSION_NOT_FOUND
	// when there is none.
	GetDesignSystemVersion(ctx context.Context, projectID string, version int) (*domain.DesignSystemVersion, error)
}

// DesignSystemStore keeps every version of every project's design system
// (driven). Versions are never changed once written.
type DesignSystemStore interface {
	// Latest answers the project's newest version, or nil when it has none.
	Latest(ctx context.Context, projectID string) (*domain.DesignSystemVersion, error)
	// Get answers one version, or nil when there is none.
	Get(ctx context.Context, projectID string, version int) (*domain.DesignSystemVersion, error)
	// List answers up to limit versions older than before (every one when 0),
	// newest first, with their records.
	List(ctx context.Context, projectID string, limit, before int) ([]*domain.DesignSystemVersion, error)
	// Append writes v as version baseVersion+1 when the newest is baseVersion
	// (0 when there is none), else DESIGN_SYSTEM_CHANGED with
	// details.current_version. It sets v.Version.
	Append(ctx context.Context, v *domain.DesignSystemVersion, baseVersion int) error
	// DeleteByProject removes every version of the project.
	DeleteByProject(ctx context.Context, projectID string) error
}

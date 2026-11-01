package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// DesignSystems is the design system use cases: a project holds many named
// design systems (ADR 0057), each a structured record written only by the
// person here and kept as immutable versions. *designsystem.Service
// satisfies it.
//
// An unknown project is PROJECT_NOT_FOUND; an unknown design system is
// DESIGN_SYSTEM_NOT_FOUND.
type DesignSystems interface {
	// DesignSystemReader is the bridge for consumers that cannot name a
	// design system yet (tickets 11-13 re-key them).
	DesignSystemReader

	// ListDesignSystems lists the project's design systems ordered by name.
	ListDesignSystems(ctx context.Context, projectID string) ([]*domain.DesignSystemSummary, error)
	// CreateDesignSystem stores a design system's identity only (version 0,
	// no history). DESIGN_SYSTEM_INVALID carries the field errors; SLUG_TAKEN
	// (details.field "slug" or "name") says the project has one like it.
	CreateDesignSystem(ctx context.Context, projectID, name, slug, description string) (*domain.DesignSystemIdentity, error)
	// UpdateDesignSystem changes the name and/or the description (nil leaves
	// it); the slug never changes. It answers the design system as GetDesignSystem does.
	UpdateDesignSystem(ctx context.Context, designSystemID string, name, description *string) (*domain.DesignSystem, error)
	// DeleteDesignSystem removes the design system and its versions when
	// confirmName is its name (else INVALID_INPUT).
	DeleteDesignSystem(ctx context.Context, designSystemID, confirmName string) error

	// GetDesignSystem is the latest version's record, or the empty record
	// (Exists false, version 0) when none was saved. It stores nothing.
	GetDesignSystem(ctx context.Context, designSystemID string) (*domain.DesignSystem, error)
	// SaveDesignSystem writes record as the next version (source manual)
	// when baseVersion is the current version (0 for the first save).
	// DESIGN_SYSTEM_INVALID carries the field errors; DESIGN_SYSTEM_CHANGED
	// says the current version moved.
	SaveDesignSystem(ctx context.Context, designSystemID string, baseVersion int, record domain.DesignSystemRecord, message string) (*domain.DesignSystemVersion, error)
	// RenameDesignToken renames a token and rewrites the previews that use it
	// as one version, with the same refusals as a save.
	RenameDesignToken(ctx context.Context, designSystemID string, baseVersion int, group, name, newGroup, newName string) (*domain.DesignSystemVersion, error)
	// ListDesignSystemVersions lists versions newest first, records included
	// (callers that list drop them): at most limit of them (a default when 0),
	// older than before (every one when 0).
	ListDesignSystemVersions(ctx context.Context, designSystemID string, limit, before int) ([]*domain.DesignSystemVersion, error)
	// GetDesignSystemVersion is one version with its record; VERSION_NOT_FOUND
	// when there is none.
	GetDesignSystemVersion(ctx context.Context, designSystemID string, version int) (*domain.DesignSystemVersion, error)
}

// DesignSystemStore keeps the identity of every project's design systems and
// every version of them (driven). Versions are never changed once written.
type DesignSystemStore interface {
	// Create stores the identity (it sets ID and the times). SLUG_TAKEN with
	// details.field when the project has a design system with that slug or
	// name.
	Create(ctx context.Context, d *domain.DesignSystemIdentity) error
	// Update stores the identity's name and description (never the slug), with
	// the same SLUG_TAKEN for a name another design system has.
	Update(ctx context.Context, d *domain.DesignSystemIdentity) error
	// Identity answers one design system, or nil when there is none.
	Identity(ctx context.Context, designSystemID string) (*domain.DesignSystemIdentity, error)
	// ListByProject answers the project's identities in creation order.
	ListByProject(ctx context.Context, projectID string) ([]*domain.DesignSystemIdentity, error)
	// Delete removes the design system and its versions.
	Delete(ctx context.Context, designSystemID string) error
	// DeleteByProject removes every design system of the project and their versions.
	DeleteByProject(ctx context.Context, projectID string) error

	// Latest answers the design system's newest version, or nil when it has none.
	Latest(ctx context.Context, designSystemID string) (*domain.DesignSystemVersion, error)
	// Get answers one version, or nil when there is none.
	Get(ctx context.Context, designSystemID string, version int) (*domain.DesignSystemVersion, error)
	// List answers up to limit versions older than before (every one when 0),
	// newest first, with their records.
	List(ctx context.Context, designSystemID string, limit, before int) ([]*domain.DesignSystemVersion, error)
	// Append writes v as version baseVersion+1 when the newest is baseVersion
	// (0 when there is none), else DESIGN_SYSTEM_CHANGED with
	// details.current_version. It sets v.Version.
	Append(ctx context.Context, v *domain.DesignSystemVersion, baseVersion int) error
}

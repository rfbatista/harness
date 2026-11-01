package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// DesignSystems is the design system use cases: a project holds many named
// design systems (ADR 0060), each a structured record written only by the
// person here and kept as immutable versions. *designsystem.Service
// satisfies it.
//
// An unknown project is PROJECT_NOT_FOUND; an unknown design system is
// DESIGN_SYSTEM_NOT_FOUND.
type DesignSystems interface {
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

// DesignSystemPages is what a design system's page reads: the project's design
// systems to find one by slug, and the one found, record included. It stores
// nothing. *designsystem.Service satisfies it.
type DesignSystemPages interface {
	ListDesignSystems(ctx context.Context, projectID string) ([]*domain.DesignSystemSummary, error)
	GetDesignSystem(ctx context.Context, designSystemID string) (*domain.DesignSystem, error)
}

// DesignSystemStamp is a design system's newest version number and its time.
type DesignSystemStamp struct {
	Version int
	At      time.Time
}

// DesignSystemStore keeps the identity of every project's design systems and
// every version of them (driven). Versions are never changed once written.
type DesignSystemStore interface {
	// Create stores the identity (it sets ID and the times). SLUG_TAKEN with
	// details.field when the project has a design system with that slug or
	// name.
	Create(ctx context.Context, d *domain.DesignSystemIdentity) error
	// CreateWithVersion stores the identity and its first version (v, whose
	// DesignSystemID it sets) together or not at all, with Create's SLUG_TAKEN.
	CreateWithVersion(ctx context.Context, d *domain.DesignSystemIdentity, v *domain.DesignSystemVersion) error
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
	// LatestStamps answers the newest version number and time of each of the
	// project's design systems that has one, without their records.
	LatestStamps(ctx context.Context, projectID string) (map[string]DesignSystemStamp, error)
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

// DesignSourceRepositories is what a design system import reads of the
// project's repositories. *projects.Service satisfies it.
type DesignSourceRepositories interface {
	ListRepositories(ctx context.Context, projectID string) ([]*domain.Repository, error)
	GetRepository(ctx context.Context, id string) (*domain.Repository, error)
}

// DesignSource is a repository whose default branch holds design system
// files, with the paths found ("" for a file it lacks).
type DesignSource struct {
	RepositoryID string
	Name         string
	DesignMD     string
	Tokens       string
}

// DesignImportRequest names what an import reads: a repository of the
// project and three paths ("" for the defaults DESIGN.md, tokens.json and
// components/).
type DesignImportRequest struct {
	ProjectID     string
	RepositoryID  string
	DesignMDPath  string
	TokensPath    string
	ComponentsDir string
}

// DesignImportSkip is one entry the import left out, and why.
type DesignImportSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// DesignImportPreview is what an import would store: the record, what was
// found, the tokens counted by group and what was skipped.
type DesignImportPreview struct {
	FoundDesignMD   bool
	FoundTokens     bool
	FoundComponents int
	Record          domain.DesignSystemRecord
	Counts          map[string]int
	Skipped         []DesignImportSkip
}

// DesignSystemImports reads design system files from a repository's default
// branch and creates a new design system from them (ADR 0060: an import never
// writes into an existing one). *designsystem.Service satisfies it.
//
// IMPORT_UNREADABLE says neither DESIGN.md nor tokens.json parsed;
// CROSS_PROJECT_ACCESS says the repository is another project's.
type DesignSystemImports interface {
	// FindDesignSystemSources lists the repositories whose default branch holds
	// a DESIGN.md or a tokens.json at the root or one level down.
	FindDesignSystemSources(ctx context.Context, projectID string) ([]DesignSource, error)
	// PreviewDesignSystemImport reads and parses; it never writes.
	PreviewDesignSystemImport(ctx context.Context, req DesignImportRequest) (*DesignImportPreview, error)
	// ImportDesignSystem creates a design system whose version 1 is the
	// import, all or nothing. SLUG_TAKEN as CreateDesignSystem.
	ImportDesignSystem(ctx context.Context, req DesignImportRequest, name, slug, description string) (*domain.DesignSystemIdentity, *domain.DesignSystemVersion, error)
}

// DesignExportGit is what a design system export asks of a checkout: the
// architecture export's reads and commit, plus a folder read whole and a commit
// that also deletes (driven; gitcli implements it).
type DesignExportGit interface {
	ArchitectureGit
	// ReadTree reads every file branch tracks under prefix, keyed by its path
	// relative to prefix; empty when there is none.
	ReadTree(ctx context.Context, dir, branch, prefix string) (map[string][]byte, error)
	// CommitChanges writes files and deletes removed, both in the checkout
	// (which is on the default branch and clean), and commits only those paths
	// with message, as CommitFiles does. It never pushes. It answers the new
	// commit.
	CommitChanges(ctx context.Context, dir string, files map[string][]byte, removed []string, message string) (string, error)
}

// DesignExportRecord is what the last export of a repository wrote: the design
// system and version, the commit and time, and the hash of the files.
type DesignExportRecord struct {
	RepositoryID   string
	DesignSystemID string
	Version        int
	Commit         string
	At             time.Time
	SHA            string
}

// DesignExportStore keeps the last export of each repository (driven).
type DesignExportStore interface {
	// Get answers the repository's last export, or nil when it has none.
	Get(ctx context.Context, repositoryID string) (*DesignExportRecord, error)
	// Set replaces the repository's last export.
	Set(ctx context.Context, rec *DesignExportRecord) error
	// Delete forgets the repository's last export.
	Delete(ctx context.Context, repositoryID string) error
}

// DesignExportResult is what an export answers.
type DesignExportResult struct {
	Commit  string `json:"commit"`
	Version int    `json:"version"`
	State   string `json:"state"`
}

// Export states of a repository against the design system it applies.
const (
	DesignExportNeverExported = "never_exported"
	DesignExportInSync        = "in_sync"
	DesignExportBehind        = "behind"
	DesignExportEditedOutside = "edited_outside"
)

// DesignExportInfo is where one repository stands against the design system
// it applies. The exported fields are null before the first export.
type DesignExportInfo struct {
	State           string     `json:"state"`
	ExportedVersion *int       `json:"exported_version"`
	CurrentVersion  int        `json:"current_version"`
	ExportedCommit  string     `json:"exported_commit,omitempty"`
	ExportedAt      *time.Time `json:"exported_at,omitempty"`
	ChangedFiles    []string   `json:"changed_files,omitempty"`
}

// DesignExportFile is one file an export would write or delete, with how it
// differs from the checkout.
type DesignExportFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added | changed | unchanged | deleted
	Diff   string `json:"diff"`
}

// DesignSystemExports writes a design system into the repositories that use
// it, on explicit request (ADR 0056). *designsystem.Service satisfies it.
//
// NO_DESIGN_SYSTEM says the repository applies none; CHECKOUT_DIRTY and
// WRONG_BRANCH are the architecture export's; EDITED_OUTSIDE (details
// changed_files) says the files differ from what was exported.
type DesignSystemExports interface {
	// ListDesignSystemExports answers, for each repository that applies the
	// design system (keyed by repository id), where its export stands.
	ListDesignSystemExports(ctx context.Context, designSystemID string) (map[string]*DesignExportInfo, error)
	// PreviewDesignSystemExport lists the files an export would write or
	// delete at the repository's path, each with its diff against the default
	// branch. It writes nothing.
	PreviewDesignSystemExport(ctx context.Context, repositoryID string) ([]DesignExportFile, error)
	// ExportDesignSystem commits the repository's design system files at its
	// path on the default branch of the local checkout, without pushing.
	// Edited-outside files are overwritten only when confirmEditedOutside.
	ExportDesignSystem(ctx context.Context, repositoryID string, confirmEditedOutside bool) (*DesignExportResult, error)
}

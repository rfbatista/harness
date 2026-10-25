package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// RepositoryArchitectures is the repository architecture use cases: one
// structured record per repository, saved only by the person, read by
// sessions and hooks. *repoarch.Service satisfies it.
//
// An unknown repository is REPOSITORY_NOT_FOUND.
type RepositoryArchitectures interface {
	// GetRepositoryArchitecture is the stored record, or the starter record
	// (Exists false, version 0) when none is saved.
	GetRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.RepositoryArchitecture, error)
	// SaveRepositoryArchitecture stores record as a new version when
	// baseVersion is the stored version (0 to create), and replaces the
	// repository's layer zones in the same transaction. ARCHITECTURE_INVALID
	// carries the field errors; ARCHITECTURE_CHANGED says the stored version
	// moved.
	SaveRepositoryArchitecture(ctx context.Context, repositoryID string, baseVersion int, record domain.ArchitectureRecord) (*domain.RepositoryArchitecture, error)
	// DeleteRepositoryArchitecture removes the record and its layer zones.
	DeleteRepositoryArchitecture(ctx context.Context, repositoryID string) error
	// PreviewLayers says what an unsaved draft of the layers would catch in
	// the checkout's default branch. It stores nothing.
	PreviewLayers(ctx context.Context, repositoryID string, layers []domain.ArchLayer) (*domain.LayersPreview, error)
	// DiscoverRepositoryArchitecture finds instruction files, tools and
	// sandboxes in the checkout and the harness, for the person to confirm.
	// It never writes.
	DiscoverRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.ArchitectureDiscovery, error)
	// ExportRepositoryArchitecture writes .harness/architecture.yaml and its
	// schema into the repository's local checkout and commits them on the
	// default branch, without pushing. It refuses with ARCHITECTURE_CHANGED
	// (version is not the current one), ARCHITECTURE_NOT_FOUND, WRONG_BRANCH
	// and CHECKOUT_DIRTY. Files that already hold the version's content commit
	// nothing and answer the commit that has them.
	ExportRepositoryArchitecture(ctx context.Context, repositoryID string, version int) (*domain.ArchitectureExportResult, error)
	ArchitectureRuleEvents
}

// StoredArchitecture is a repository's record row.
type StoredArchitecture struct {
	RepositoryID string
	ProjectID    string
	Version      int
	Record       domain.ArchitectureRecord
	UpdatedAt    time.Time
	// The last export: its version, commit, time and the SHA-256 of the file
	// it wrote.
	ExportedVersion int
	ExportedCommit  string
	ExportedAt      *time.Time
	ExportedSHA     string
}

// ArchitectureRecordStore keeps the records and their layer zones (driven).
type ArchitectureRecordStore interface {
	// Get answers the repository's row, or nil when it has none.
	Get(ctx context.Context, repositoryID string) (*StoredArchitecture, error)
	// Save stores record as the next version, replacing the repository's
	// layer zones with zones, in one transaction. It refuses with
	// ARCHITECTURE_CHANGED when the stored version is not baseVersion (0 when
	// there is none). It answers the new version.
	Save(ctx context.Context, repositoryID, projectID string, baseVersion int, record domain.ArchitectureRecord, zones []*domain.Zone) (*StoredArchitecture, error)
	// Delete removes the record and the repository's layer zones; no error
	// when it has none.
	Delete(ctx context.Context, repositoryID string) error
	// DeleteByProject does the same for every repository of the project.
	DeleteByProject(ctx context.Context, projectID string) error
	// ListByProject answers the records of the project's repositories.
	ListByProject(ctx context.Context, projectID string) ([]*StoredArchitecture, error)
	// SetExport records the last export: its version, commit, time and the
	// SHA-256 of the YAML it wrote. It is not a new version of the record.
	SetExport(ctx context.Context, repositoryID string, version int, commit string, at time.Time, sha string) (*StoredArchitecture, error)
}

// ArchitectureGit reads a checkout's default branch for the architecture
// (driven; gitcli implements it).
type ArchitectureGit interface {
	// DefaultBranch is the checkout's default branch: origin's HEAD, main,
	// master, else the branch checked out. "" when it has none.
	DefaultBranch(ctx context.Context, dir string) (string, error)
	// Files lists the files the branch tracks.
	Files(ctx context.Context, dir, branch string) ([]string, error)
	// ReadFile reads path as the branch has it; false when it has no such
	// file.
	ReadFile(ctx context.Context, dir, branch, path string) ([]byte, bool, error)
	// Checkout reads where the checkout is: its branch and what is
	// uncommitted.
	Checkout(ctx context.Context, dir string) (CheckoutState, error)
	// LastCommit is the newest commit of branch that touched path; "" when
	// none did.
	LastCommit(ctx context.Context, dir, branch, path string) (string, error)
	// CommitFiles writes files into the checkout (which is on branch and
	// clean) and commits only them with message, authored by the harness and
	// without running the checkout's hooks. It never pushes. On failure the
	// checkout is as it was. It answers the new commit.
	CommitFiles(ctx context.Context, dir string, files map[string][]byte, message string) (string, error)
}

// ArchitectureRuleEventStore keeps the rules that failed (driven). Events are
// kept as long as their session is.
type ArchitectureRuleEventStore interface {
	Add(ctx context.Context, ev *domain.ArchitectureRuleEvent) error
	// List answers the repository's events newest first: those after since
	// (all when zero), at most limit.
	List(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error)
	DeleteBySession(ctx context.Context, sessionID string) error
}

// ArchitectureRuleEvents lists a repository's rule events: the driving side
// of the store, for the API.
type ArchitectureRuleEvents interface {
	// ListArchitectureRuleEvents is the newest first, after since when it is
	// set, at most limit (default 50, at most 500).
	ListArchitectureRuleEvents(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error)
}

// ArchitectureCheck is what a hook, a shim or the commit check asks about:
// the session, the lifecycle point, the tool, and the paths it touches or has
// staged (absolute, or relative to the session's worktree).
type ArchitectureCheck struct {
	SessionID string
	// Point is domain.PointBeforeTool, PointAfterEdit or PointBeforeCommit.
	Point string
	Tool  string
	Paths []string
	// Command is the Bash command line of a before-tool check of Bash.
	Command string
}

// ArchitectureVerdict is the answer: Deny is the text of a blocking rule that
// failed (the action must not go on); Context is everything else the agent
// should be told (warnings, and a blocking rule's text after an edit). Both
// empty means every rule passed.
type ArchitectureVerdict struct {
	Deny    string `json:"deny,omitempty"`
	Context string `json:"context,omitempty"`
}

// ArchitectureRules checks architecture rules against the repository's
// current saved record (driving). It never errors: a check it cannot make is a
// pass, so a harness failure never blocks an agent.
type ArchitectureRules interface {
	CheckArchitecture(ctx context.Context, check ArchitectureCheck) ArchitectureVerdict
}

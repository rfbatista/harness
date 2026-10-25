package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// A task session's virtual shell: a temporary folder holding its task's and
// project's documents, comments, resources and designs as files (see
// "Contract: Harness server ↔ Agent sessions — virtual shell").

// ShellFile is one file of a virtual shell as the session last received it:
// a row of its .index.json, and the file's base version for that session.
// Path is relative to the shell, with forward slashes.
type ShellFile struct {
	Path      string    `json:"path"`
	Kind      string    `json:"kind"`
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
	Version   string    `json:"version"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
	// Repository names the repository of an architecture file.
	Repository string `json:"repository,omitempty"`
}

// The kinds of file a virtual shell holds.
const (
	ShellReadme    = "readme"
	ShellTask      = "task"
	ShellDocument  = "document"
	ShellComment   = "comment"
	ShellResource  = "resource"
	ShellLinks     = "links"
	ShellArtifact  = "artifact"
	ShellAttached  = "attached"
	ShellDesignURL = "urls"
	// ShellArchitecture is a repository architecture record as YAML.
	ShellArchitecture = "architecture"
)

// VirtualShells makes and removes task sessions' virtual shells.
type VirtualShells interface {
	// Open makes the session's shell, fills it with its task's and
	// project's resources (its project's alone for a shell with no task),
	// records what it was filled with as the session's base versions, and
	// answers the shell's absolute path. A session opened again gets a
	// fresh shell; the old one is removed.
	Open(ctx context.Context, sessionID string, scope ShellScope) (string, error)
	// Close removes the shell at dir, and forgets the session's base
	// versions while dir is still its shell, so closing an old shell never
	// touches a newer one. A shell already gone is not an error.
	Close(sessionID, dir string)
}

// ShellBaseVersions is what each running shell was last given, per session.
type ShellBaseVersions interface {
	// BaseVersion is the file at path as the session last received it;
	// false when the session has no shell or the shell no such file.
	BaseVersion(sessionID, path string) (ShellFile, bool)
	// ShellFiles is every file of the session's shell, by path; nil when it
	// has none.
	ShellFiles(sessionID string) []ShellFile
}

// ShellShimCommands are the commands a task session runs through the
// harness's PATH shims (contract §5).
var ShellShimCommands = []string{"sed", "tee", "cp", "mv", "rm", "touch", "mkdir"}

// ShimDirEnv names, in a session's environment, its PATH shims' directory.
// A shell that runs a profile before the program puts it back first on
// PATH, so a profile that reorders PATH cannot bypass the shims.
const ShimDirEnv = "HARNESS_SHIM_DIR"

// DeveloperBinEnv names, in a session's environment, the active developer
// directory's usr/bin on macOS. It goes right after the shims on PATH, so
// git and clang run directly and not through /usr/bin's xcrun shim, whose
// shared cache the sandbox keeps read-only. A login shell puts it back there
// after its profile.
const DeveloperBinEnv = "HARNESS_DEVELOPER_BIN"

// SaverBinDirsEnv names, in an agent process's environment, the directories
// of the active token savers' binaries, a PATH-style list. They go on PATH
// right after the shims, before the keychain shim; a login shell puts them
// back there after its profile.
const SaverBinDirsEnv = "HARNESS_SAVER_BIN_DIRS"

// SecurityShimCommand is the macOS `security` shim, a link to the shim
// binary: it answers "not found" for Claude's keychain item and passes every
// other call to the real /usr/bin/security (ADR 0022).
const SecurityShimCommand = "security"

// KeychainShimDirEnv names, in an agent process's environment, the directory
// holding the security shim. It goes on PATH right after the edit-proxy
// shims and before the developer bin; a login shell puts it back there.
const KeychainShimDirEnv = "HARNESS_KEYCHAIN_SHIM_DIR"

// The environment a task session's shell is named in: ShellDirEnv is the
// shell's absolute path, ShellURLEnv its edit endpoints' base
// (…/api/sessions/{id}/shell), which the PATH shims call.
const (
	ShellDirEnv = "HARNESS_SHELL"
	ShellURLEnv = "HARNESS_SHELL_URL"
)

// The environment the architecture rules reach the PATH shims by:
// ArchitectureURLEnv is the session's architecture endpoints' base
// (…/api/sessions/{id}/architecture), WorktreeEnv the worktree whose writes
// they check.
const (
	ArchitectureURLEnv = "HARNESS_ARCHITECTURE_URL"
	WorktreeEnv        = "HARNESS_WORKTREE"
)

// ShellUnavailable is the refusal when the harness cannot be reached before
// a change is made (contract §6).
const ShellUnavailable = "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written"

// ShellUnavailableAfter is the refusal when the harness cannot be reached
// after rel was changed on disk (contract §6).
func ShellUnavailableAfter(rel string) string {
	return "SHELL_UNAVAILABLE: the harness could not be reached; " + rel + " changed on disk but was not stored — redo the change once the harness is back"
}

// ShellEdit is one file operation an agent makes in its shell, as the edit
// proxy (hooks and PATH shims) reports it: Op is write (or create, modify,
// touch), rm, mv or cp (Paths[0] the source, Paths[1] the destination), or
// mkdir; redirect is a shell redirect into Paths, refused inside the shell
// with REDIRECT_DENIED. Paths are absolute; a relative one is taken as outside the shell.
// Tool names the tool or command that makes it.
type ShellEdit struct {
	Op    string   `json:"op"`
	Paths []string `json:"paths"`
	Tool  string   `json:"tool,omitempty"`
}

// ShellAnswer is the harness's answer to a check or a commit. Allow false
// carries the refusal's code and its reason, which starts with the code
// (contract §6). Paths outside the shell are always allowed, with nothing
// else said.
type ShellAnswer struct {
	Allow  bool   `json:"allow"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ShellEdits checks and stores what agents do to their shells' files.
type ShellEdits interface {
	// CheckShellEdit says whether the session may make e, before it does.
	CheckShellEdit(ctx context.Context, sessionID string, e ShellEdit) ShellAnswer
	// CommitShellEdit stores e once the session has made it, reading the
	// bytes from disk; the stored version becomes the session's base
	// version, and every other running shell gets it.
	CommitShellEdit(ctx context.Context, sessionID string, e ShellEdit) ShellAnswer
}

// ShellDocumentWriter stores what agents do to their shells' document
// files. Planning implements it. WriteDocumentIfVersion replaces a
// document's title, content and format while its content is still at
// version, and is DOCUMENT_CHANGED otherwise.
type ShellDocumentWriter interface {
	GetDocument(id string) *domain.Document
	CreateDocument(projectID, title, content string, format domain.DocumentFormat, scope domain.DocumentScope) (*domain.Document, error)
	WriteDocumentIfVersion(id, version, title, content string, format domain.DocumentFormat) (*domain.Document, error)
	SetDocumentScope(id string, scope domain.DocumentScope) (*domain.Document, error)
	DeleteDocument(id string) error
	LinkDocument(ticketID, documentID string) error
}

// ShellScope is what a session's shell is opened on: its task, or, with no
// TicketID, its project alone (a project-level product session, ADR 0016).
// ProjectID is read only when TicketID is empty; a task's shell is in the
// task's project.
type ShellScope struct {
	ProjectID string
	TicketID  string
}

// ArchitectureFile is a repository architecture record as the virtual shell
// shows it: its YAML form and its version.
type ArchitectureFile struct {
	RepositoryID string
	Repository   string
	Version      int
	YAML         []byte
	UpdatedAt    time.Time
}

// ArchitectureFileReader lists the architecture files of a project's
// repositories that have a record, in the repositories' order.
type ArchitectureFileReader interface {
	ListArchitectureFiles(ctx context.Context, projectID string) ([]ArchitectureFile, error)
}

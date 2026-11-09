package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// A task session's resources root: a temporary folder holding its task's and
// project's documents, comments, resources and designs as files, one root of
// its virtual file system (ADR 0050; see "Contract: Harness server ↔ Agent
// sessions — virtual shell").

// ShellFile is one file of a resources root as the session last received it:
// a row of its .index.json, and the file's base version for that session.
// Path is relative to the resources root, with forward slashes.
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
	// Writable says the session may write the file: only the files of a
	// design session's draft are, and only those the harness does not generate.
	Writable bool `json:"writable,omitempty"`
}

// The kinds of file a resources root holds.
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
	// ShellDesignSystem is a file of the project's design system view.
	ShellDesignSystem = "design-system"
	// ShellInstruction is one instruction file of a repository (ADR 0057).
	ShellInstruction = "instruction"
	// ShellRepositoryPrompt is one agent's repository prompt (ADR 0061).
	ShellRepositoryPrompt = "repository-prompt"
)

// ResourcesRoots makes and removes task sessions' resources roots.
type ResourcesRoots interface {
	// Open makes the session's resources root, fills it with its task's and
	// project's resources (its project's alone for a root with no task),
	// records what it was filled with as the session's base versions, and
	// answers the root's absolute path. A session opened again gets a
	// fresh root; the old one is removed.
	Open(ctx context.Context, sessionID string, scope ShellScope) (string, error)
	// Close removes the root at dir, and forgets the session's base
	// versions while dir is still its root, so closing an old root never
	// touches a newer one. A root already gone is not an error.
	Close(sessionID, dir string)
}

// BaseVersions is what each running resources root was last given, per session.
type BaseVersions interface {
	// BaseVersion is the file at path as the session last received it;
	// false when the session has no root or the root no such file.
	BaseVersion(sessionID, path string) (ShellFile, bool)
	// ShellFiles is every file of the session's root, by path; nil when it
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

// The environment a task session's resources root is named in: ShellDirEnv
// is the root's absolute path, ShellURLEnv its edit endpoints' base
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

// BranchGateShimCommand is the git shim, a link to the shim binary in every
// agent session's shim dir: it refuses a branch action that has no branch grant
// (contract "Harness server ↔ Agent sessions — branch gate" §3).
const BranchGateShimCommand = "git"

// BranchGateURLEnv names, in a session's environment, its branch gate's base
// (…/api/sessions/{id}/branch_gate), which the git shim asks for a grant.
const BranchGateURLEnv = "HARNESS_BRANCH_GATE_URL"

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
// mkdir; redirect is a shell redirect into Paths, refused inside the resources root
// with REDIRECT_DENIED. Paths are absolute; a relative one is taken as outside the resources root.
// Tool names the tool or command that makes it.
type ShellEdit struct {
	Op    string   `json:"op"`
	Paths []string `json:"paths"`
	Tool  string   `json:"tool,omitempty"`
}

// ShellAnswer is the harness's answer to a check or a commit. Allow false
// carries the refusal's code and its reason, which starts with the code
// (contract §6). Paths outside the resources root are always allowed, with nothing
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
// ProjectID is read only when TicketID is empty; a task's root is in the
// task's project.
type ShellScope struct {
	ProjectID string
	TicketID  string
	// RepositoryID is the repository the session works in, "" for none: the
	// shell shows the project design system when it uses it.
	RepositoryID string
	// DesignSystemID is the design system a design session is bound to: its
	// shell holds that design system's draft, writable.
	DesignSystemID string
}

// ArchitectureFile is a repository's architecture as the resources root
// shows it: the record's YAML form and version when it has a record, and its
// instruction files.
type ArchitectureFile struct {
	RepositoryID string
	Repository   string
	// Folder is the repository's name in the resources root, the same for
	// every repository's file and with -<short id> for a name an earlier
	// repository has.
	Folder string
	// HasRecord is false for a repository that has instruction files only;
	// Version, YAML and UpdatedAt are then empty.
	HasRecord    bool
	Version      int
	YAML         []byte
	UpdatedAt    time.Time
	Instructions []InstructionShellFile
}

// InstructionShellFile is an instruction file as the resources root shows it.
type InstructionShellFile struct {
	Path      string
	Version   int
	Content   []byte
	UpdatedAt time.Time
}

// ArchitectureFileReader lists the architecture files of a project's
// repositories that have a record, in the repositories' order.
type ArchitectureFileReader interface {
	ListArchitectureFiles(ctx context.Context, projectID string) ([]ArchitectureFile, error)
}

// RepositoryPromptFile is a repository prompt as the resources root shows it,
// at project/architecture/<repository>/agents/<agent>.md.
type RepositoryPromptFile struct {
	RepositoryID string
	Repository   string
	Agent        string
	// Version is the prompt's stored version.
	Version   string
	Content   []byte
	UpdatedAt time.Time
}

// RepositoryPromptFileReader lists the repository prompts that are set in a
// project's repositories, in the repositories' order.
type RepositoryPromptFileReader interface {
	ListRepositoryPromptFiles(ctx context.Context, projectID string) ([]RepositoryPromptFile, error)
}

// DesignSystemByIDReader reads one design system, whole, by its id:
// DESIGN_SYSTEM_NOT_FOUND when there is none.
type DesignSystemByIDReader interface {
	GetDesignSystem(ctx context.Context, designSystemID string) (*domain.DesignSystem, error)
}

// The Files view's reads of a session's virtual file system (see "Contract:
// Harness server ↔ Web UI — a session's virtual file system"). They are
// read-only: nothing here creates, renames, deletes or uploads a file.

// The agent states a session's Files view shows.
const (
	AgentWorking = "working"
	AgentWaiting = "waiting"
	AgentIdle    = "idle"
	AgentEnded   = "ended"
)

// SessionRoot is one root of a session's virtual file system as the Files
// view lists it. Note says why an unavailable root cannot be browsed.
type SessionRoot struct {
	Root      string `json:"root"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
	Note      string `json:"note"`
}

// SessionRoots is the answer of the roots route: both roots, the worktree
// first.
type SessionRoots struct {
	SessionID  string        `json:"session_id"`
	AgentState string        `json:"agent_state"`
	Roots      []SessionRoot `json:"roots"`
}

// SessionEntriesQuery asks for one page of a folder's entries. Dir is
// relative to the root ("" the root itself); After is the name the previous
// page ended on; Limit is 0 for the default.
type SessionEntriesQuery struct {
	Root  string
	Dir   string
	After string
	Limit int
}

// SessionEntry is one entry of a folder. Size is set on a file, Ignored on a
// folder (git's ignore rules, worktree root only), Target on a symlink.
type SessionEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Ignored *bool  `json:"ignored,omitempty"`
	Size    *int64 `json:"size,omitempty"`
	Target  string `json:"target,omitempty"`
}

// SessionEntries is one page of a folder, directories first then files, each
// by name. Next is the cursor of the following page, empty on the last.
type SessionEntries struct {
	Root    string         `json:"root"`
	Dir     string         `json:"dir"`
	Entries []SessionEntry `json:"entries"`
	Next    string         `json:"next,omitempty"`
}

// SessionFiles is the virtual file system as the web UI browses it (driving;
// the virtual file system domain implements it). Refusals: SESSION_NOT_FOUND,
// FILES_NOT_AVAILABLE (no workspace), INVALID_INPUT (an unclean path, an
// unknown root), ROOT_GONE (the root is not available), PATH_NOT_FOUND.
type SessionFiles interface {
	SessionRoots(ctx context.Context, sessionID string) (*SessionRoots, error)
	SessionEntries(ctx context.Context, sessionID string, q SessionEntriesQuery) (*SessionEntries, error)
	// SessionFile reads one file the way the Code tab does: its content up to
	// the Code tab's cap, with its binary and truncated flags.
	SessionFile(ctx context.Context, sessionID, root, path string) (*domain.CodeFile, error)
}

// DesignDraftFile is one file of a design session's draft.
type DesignDraftFile struct {
	// Path is relative to project/design-system/.
	Path    string
	Content []byte
	// Editable is false for the files the harness generates alone.
	Editable bool
}

// DesignDraftView is a design session's draft as its shell shows it: the
// files the draft would export, and whether a proposal made from it waits.
type DesignDraftView struct {
	Files     []DesignDraftFile
	Submitted bool
}

// DesignDrafts is what a design session's shell reads and writes of its
// draft (contract "Harness server ↔ Agent sessions — design-system view,
// draft and proposals"). A write the draft cannot take is refused with the
// reason in the error's message.
type DesignDrafts interface {
	// DesignDraftFiles answers the draft of the design system the design
	// session is bound to; nil when designSystemID is empty.
	DesignDraftFiles(ctx context.Context, sessionID, designSystemID string) (*DesignDraftView, error)
	// WriteDraftFile applies the file written at rel to the draft.
	WriteDraftFile(ctx context.Context, sessionID, designSystemID, rel string, content []byte) error
	// RemoveDraftFile applies the removal of rel to the draft.
	RemoveDraftFile(ctx context.Context, sessionID, designSystemID, rel string) error
}

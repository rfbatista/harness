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
)

// VirtualShells makes and removes task sessions' virtual shells.
type VirtualShells interface {
	// Open makes the session's shell, fills it with its task's and
	// project's resources, records what it was filled with as the
	// session's base versions, and answers the shell's absolute path. A
	// session opened again gets a fresh shell; the old one is removed.
	Open(ctx context.Context, sessionID, ticketID string) (string, error)
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

// ShellEdit is one file operation an agent makes in its shell, as the edit
// proxy (hooks and PATH shims) reports it: Op is write (or create, modify,
// touch), rm, mv or cp (Paths[0] the source, Paths[1] the destination), or
// mkdir. Paths are absolute; a relative one is taken as outside the shell.
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

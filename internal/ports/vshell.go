package ports

import (
	"context"
	"time"
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

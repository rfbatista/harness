package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// CodeSourceQuery names the code a view reads: exactly one of RepositoryID
// (at Ref, the main checkout's branch when empty) or WorkspaceID (the
// worktree as it is on disk).
type CodeSourceQuery struct {
	RepositoryID string
	Ref          string
	WorkspaceID  string
}

// CodeDiffQuery names a diff: WorkspaceID with Scope (all when empty),
// RepositoryID with Commit, or RepositoryID with Base and Head. Path narrows
// it to one file.
type CodeDiffQuery struct {
	WorkspaceID  string
	Scope        string
	RepositoryID string
	Commit       string
	Base         string
	Head         string
	Path         string
}

// CodeBrowser reads code, read-only: a repository at a ref or a session's
// worktree, what a session or a commit changed, and how files divide into
// zones and bounded contexts (driving). *workspaces.Code satisfies it.
//
// Errors: INVALID_INPUT (bad source, path, ref or scope),
// REPOSITORY_NOT_FOUND, WORKSPACE_NOT_FOUND, WORKSPACE_MISSING (the worktree
// is gone), COMMIT_NOT_FOUND, REF_NOT_FOUND, PATH_NOT_FOUND.
type CodeBrowser interface {
	// CodeTree lists the direct children of the directory dir ("" the root).
	CodeTree(ctx context.Context, src CodeSourceQuery, dir string) (*domain.CodeTree, error)
	// CodePaths lists every file path, at most domain.MaxCodePaths.
	CodePaths(ctx context.Context, src CodeSourceQuery) (*domain.CodePaths, error)
	// CodeFile reads one file, its content capped at domain.MaxCodeFileBytes.
	CodeFile(ctx context.Context, src CodeSourceQuery, path string) (*domain.CodeFile, error)
	// CodeDiff reads the changed files, with hunks within the diff limits.
	CodeDiff(ctx context.Context, q CodeDiffQuery) (*domain.CodeDiff, error)
	// CodeMap counts the source's files per bounded context and zone.
	CodeMap(ctx context.Context, src CodeSourceQuery) (*domain.CodeMap, error)
}

// GitEntry is a path git knows about: in a tree at a commit, or in a
// worktree's index or among its untracked files.
type GitEntry struct {
	Path string
	// Kind is domain.EntryDir, EntryFile, EntrySymlink or EntrySubmodule.
	Kind string
	// Size is a blob's size in a tree; -1 when unknown.
	Size int64
	// Target is a symlink's target, read from its blob, in a tree.
	Target    string
	Untracked bool
}

// GitChange is one file changed between two sides, without its hunks.
type GitChange struct {
	// Status is one of the domain.Change* states.
	Status  string
	Path    string
	OldPath string
	// Added and Deleted are -1 for a binary file.
	Added   int
	Deleted int
}

// GitCode reads code out of a git checkout (driven; gitcli implements it).
// Nothing it runs writes to the repository, fetches or runs hooks. Refs,
// hashes and paths reach it already checked (domain.ValidRefName,
// domain.ValidCommitHash, domain.CleanCodePath).
type GitCode interface {
	// ResolveCommit is the full hash of the commit ref names, or
	// REF_NOT_FOUND.
	ResolveCommit(ctx context.Context, dir, ref string) (string, error)
	// CurrentBranch is the branch checked out in dir, "" when detached.
	CurrentBranch(ctx context.Context, dir string) (string, error)
	// MergeBase is the best common ancestor of two commits.
	MergeBase(ctx context.Context, dir, a, b string) (string, error)
	// FirstParent is commit's first parent, "" for a root commit.
	FirstParent(ctx context.Context, dir, commit string) (string, error)
	// EmptyTree is the hash of the empty tree, what a root commit is
	// compared with.
	EmptyTree(ctx context.Context, dir string) (string, error)
	// TreeAt lists what git tracks at commit: the direct children of the
	// directory path, or every file under it when recursive. A path that is
	// not a directory there is PATH_NOT_FOUND.
	TreeAt(ctx context.Context, dir, commit, path string, recursive bool) ([]GitEntry, error)
	// PathKind is the kind of path at commit, PATH_NOT_FOUND when absent.
	PathKind(ctx context.Context, dir, commit, path string) (GitEntry, error)
	// WorktreeEntries lists a worktree's tracked paths (with their index
	// kind) and its untracked ones that are not ignored.
	WorktreeEntries(ctx context.Context, dir string) ([]GitEntry, error)
	// Blob reads at most max bytes of commit:path and the blob's size.
	Blob(ctx context.Context, dir, commit, path string, max int64) ([]byte, int64, error)
	// Changes lists the files changed from base to head; head "" is the
	// worktree on disk, whose untracked files are not included.
	Changes(ctx context.Context, dir, base, head string) ([]GitChange, error)
	// Patch reads the hunks (3 lines of context) of paths from base to head
	// ("" the worktree). A file past maxLines changed lines or maxBytes of
	// patch comes back withheld as too_large; a binary one as binary.
	Patch(ctx context.Context, dir, base, head string, paths []string, maxLines int, maxBytes int64) (map[string]*domain.FileDiff, error)
	// Snapshot is a worktree's HEAD and a fingerprint of its working tree
	// that changes whenever a tracked or untracked (not ignored) file does.
	Snapshot(ctx context.Context, dir string) (head, fingerprint string, err error)
}

// FileStat is what a worktree path is on disk, without following it.
type FileStat struct {
	// Kind is domain.EntryDir, EntryFile or EntrySymlink; "" for anything
	// else (a socket, a device).
	Kind string
	Size int64
	// Target is a symlink's target; Escapes says it resolves outside the
	// worktree.
	Target  string
	Escapes bool
}

// WorktreeFiles reads a worktree's files on disk (driven; the filesystem
// adapter implements it). No read leaves root, and no symlink is followed:
// a path through a symlinked directory is PATH_NOT_FOUND, or INVALID_INPUT
// when the link points outside root.
type WorktreeFiles interface {
	// Exists reports whether the worktree directory is on disk.
	Exists(root string) bool
	// Stat describes path, PATH_NOT_FOUND when absent.
	Stat(root, path string) (FileStat, error)
	// Read reads at most max bytes of the regular file path and its size.
	Read(root, path string, max int64) ([]byte, int64, error)
	// Lines counts the lines of the regular file path, reading it through;
	// binary says it holds a NUL byte or invalid UTF-8.
	Lines(root, path string) (lines int, binary bool, err error)
}

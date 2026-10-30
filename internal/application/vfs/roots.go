package vfs

import (
	"context"
	"path"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// RootKind names one of the two roots of a session's virtual file system.
type RootKind string

const (
	// ResourcesRoot holds the task's and project's resources as files; it
	// exists only while the session runs.
	ResourcesRoot RootKind = "resources"
	// WorktreeRoot is the session's git worktree on disk; it outlives the
	// session until its workspace is removed.
	WorktreeRoot RootKind = "worktree"
)

// Root is one root of a session's virtual file system.
type Root struct {
	Kind RootKind
	// Path is the root's real path: its absolute path with its symlinks
	// resolved. It is empty when the session has no such root.
	Path string
	// Available says the root is on disk now.
	Available bool
}

// FileSystem is the virtual file system of the sessions: for each, its roots
// and where a folder of one really is. It knows nothing of shells.
type FileSystem struct {
	resources  *Service
	sessions   ports.SessionRepository
	workspaces ports.WorkspaceRepository
	files      ports.WorktreeFiles
	ignore     ports.GitIgnore
	events     *RootEvents
}

var _ ports.WorkspaceRemovalSink = (*FileSystem)(nil)

// NewFileSystem answers for the resources roots that resources keeps and the
// worktrees of the sessions' workspaces, and raises on events when a root goes
// away or comes back.
func NewFileSystem(resources *Service, sessions ports.SessionRepository, workspaces ports.WorkspaceRepository, files ports.WorktreeFiles, events *RootEvents) *FileSystem {
	return &FileSystem{resources: resources, sessions: sessions, workspaces: workspaces, files: files, events: events}
}

// Roots are the session's two roots, the resources root first.
func (f *FileSystem) Roots(ctx context.Context, sessionID string) []Root {
	return []Root{f.resourcesRoot(sessionID), f.worktreeRoot(ctx, sessionID)}
}

func (f *FileSystem) resourcesRoot(sessionID string) Root {
	dir, ok := f.resources.RootDir(sessionID)
	return Root{Kind: ResourcesRoot, Path: dir, Available: ok && f.files.Exists(dir)}
}

func (f *FileSystem) worktreeRoot(ctx context.Context, sessionID string) Root {
	r := Root{Kind: WorktreeRoot}
	sess := f.sessions.Get(sessionID)
	if sess == nil || sess.WorkspaceID == "" {
		return r
	}
	ws := f.workspaces.Get(ctx, sess.WorkspaceID)
	if ws == nil {
		return r
	}
	r.Path = ws.Path
	r.Available = f.files.Exists(ws.Path)
	return r
}

// RealPath is the real path of folder, a slash-separated path relative to
// the root of the session's kind ("" is the root itself). The folder must be
// clean (no "..", ".", empty or trailing segments, not absolute), exist as a
// directory and stay inside the root: no symlink is followed.
func (f *FileSystem) RealPath(ctx context.Context, sessionID string, kind RootKind, folder string) (string, error) {
	var root Root
	switch kind {
	case ResourcesRoot:
		root = f.resourcesRoot(sessionID)
	case WorktreeRoot:
		root = f.worktreeRoot(ctx, sessionID)
	default:
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "unknown root " + string(kind)}
	}
	if !root.Available {
		return "", &domain.StructuredError{Code: "ROOT_UNAVAILABLE", Message: "the session's " + string(kind) + " root is not available"}
	}
	if folder != "" && (path.Clean(folder) != folder || path.IsAbs(folder) || folder == "." || strings.HasPrefix(folder, "../") || folder == "..") {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "the folder must be a clean path inside the root"}
	}
	st, err := f.files.Stat(root.Path, folder)
	if err != nil {
		return "", err
	}
	if st.Kind != domain.EntryDir {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: folder + " is not a folder"}
	}
	return filepath.Join(root.Path, filepath.FromSlash(folder)), nil
}

// WorkspaceRemoved raises the worktree root's going away for every session of
// the workspace, running or finished: its worktree is gone from disk.
func (f *FileSystem) WorkspaceRemoved(ws *domain.Workspace) {
	for _, sess := range f.sessions.List(context.Background(), ports.SessionFilter{}) {
		if sess.WorkspaceID == ws.ID {
			f.events.raise(RootEvent{SessionID: sess.ID, Kind: WorktreeRoot, Path: ws.Path})
		}
	}
}

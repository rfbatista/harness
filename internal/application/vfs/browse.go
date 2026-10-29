package vfs

import (
	"bytes"
	"context"
	"path"
	"slices"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What the Files view reads of a session's virtual file system: its roots, a
// folder's entries a page at a time, and a file. Nothing here writes.

var _ ports.SessionFiles = (*FileSystem)(nil)

const (
	defaultEntriesLimit = 200
	maxEntriesLimit     = 1000

	resourcesEndedNote = "The resources folder ended with the session."
	worktreeRemoved    = "The worktree was removed."
	noResourcesNote    = "This session has no resources folder."
)

// UseGitIgnore has the worktree root's folders marked ignored by git's rules.
// Without it no folder is.
func (f *FileSystem) UseGitIgnore(g ports.GitIgnore) { f.ignore = g }

func refusal(code, msg string) error { return &domain.StructuredError{Code: code, Message: msg} }

// browsable is the session, or FILES_NOT_AVAILABLE when it has no workspace.
func (f *FileSystem) browsable(sessionID string) (*domain.Session, error) {
	sess := f.sessions.Get(sessionID)
	if sess == nil {
		return nil, refusal("SESSION_NOT_FOUND", "session "+sessionID+" not found")
	}
	if sess.WorkspaceID == "" {
		return nil, refusal("FILES_NOT_AVAILABLE", "the session has no files to browse")
	}
	return sess, nil
}

// agentState reduces a session's status to what the Files strip shows.
func agentState(s domain.SessionStatus) string {
	switch s {
	case domain.SessionDone, domain.SessionFailed, domain.SessionStopped:
		return ports.AgentEnded
	case domain.SessionWaitingApproval:
		return ports.AgentWaiting
	case domain.SessionIdle, domain.SessionPaused:
		return ports.AgentIdle
	default:
		return ports.AgentWorking
	}
}

func rootNote(kind RootKind, sess *domain.Session) string {
	if kind == WorktreeRoot {
		return worktreeRemoved
	}
	if agentState(sess.Status) == ports.AgentEnded {
		return resourcesEndedNote
	}
	return noResourcesNote
}

func rootLabel(kind RootKind) string {
	if kind == WorktreeRoot {
		return "Worktree"
	}
	return "Resources"
}

// SessionRoots answers both roots, the worktree first, and the agent state.
func (f *FileSystem) SessionRoots(ctx context.Context, sessionID string) (*ports.SessionRoots, error) {
	sess, err := f.browsable(sessionID)
	if err != nil {
		return nil, err
	}
	out := &ports.SessionRoots{SessionID: sessionID, AgentState: agentState(sess.Status)}
	for _, kind := range []RootKind{WorktreeRoot, ResourcesRoot} {
		r := f.root(ctx, sessionID, kind)
		sr := ports.SessionRoot{Root: string(kind), Label: rootLabel(kind), Available: r.Available}
		if !r.Available {
			sr.Note = rootNote(kind, sess)
		}
		out.Roots = append(out.Roots, sr)
	}
	return out, nil
}

func (f *FileSystem) root(ctx context.Context, sessionID string, kind RootKind) Root {
	if kind == WorktreeRoot {
		return f.worktreeRoot(ctx, sessionID)
	}
	return f.resourcesRoot(sessionID)
}

// available resolves the named root of a browsable session: INVALID_INPUT
// for an unknown root, ROOT_GONE with its note when it is not on disk.
func (f *FileSystem) available(ctx context.Context, sessionID, name string) (RootKind, Root, error) {
	sess, err := f.browsable(sessionID)
	if err != nil {
		return "", Root{}, err
	}
	kind := RootKind(name)
	if kind != WorktreeRoot && kind != ResourcesRoot {
		return "", Root{}, refusal("INVALID_INPUT", "root must be worktree or resources")
	}
	r := f.root(ctx, sessionID, kind)
	if !r.Available {
		return "", Root{}, refusal("ROOT_GONE", rootNote(kind, sess))
	}
	return kind, r, nil
}

// SessionEntries lists one page of a folder: directories first, then files
// and symlinks, each by name. In the worktree root `.git` is never listed
// and the folders git ignores are marked, with one git call for the page.
func (f *FileSystem) SessionEntries(ctx context.Context, sessionID string, q ports.SessionEntriesQuery) (*ports.SessionEntries, error) {
	kind, root, err := f.available(ctx, sessionID, q.Root)
	if err != nil {
		return nil, err
	}
	real, err := f.RealPath(ctx, sessionID, kind, q.Dir)
	if err != nil {
		return nil, err
	}
	list, err := f.files.ReadDir(real, "")
	if err != nil {
		return nil, err
	}
	var dirs, rest []ports.DirEntry
	for _, e := range list {
		switch {
		case kind == WorktreeRoot && e.Name == ".git":
		case e.Kind == domain.EntryDir:
			dirs = append(dirs, e)
		case e.Kind != "":
			rest = append(rest, e)
		}
	}
	all := append(dirs, rest...)
	start := 0
	if q.After != "" {
		start = len(dirs) // an unknown cursor resumes among the files
		if i := slices.IndexFunc(all, func(e ports.DirEntry) bool { return e.Name == q.After }); i >= 0 {
			start = i + 1
		} else if j := slices.IndexFunc(rest, func(e ports.DirEntry) bool { return e.Name > q.After }); j >= 0 {
			start = len(dirs) + j
		} else {
			start = len(all)
		}
	}
	limit := min(max(q.Limit, 0), maxEntriesLimit)
	if limit == 0 {
		limit = defaultEntriesLimit
	}
	end := min(start+limit, len(all))
	page := all[start:end]
	out := &ports.SessionEntries{Root: string(kind), Dir: q.Dir, Entries: make([]ports.SessionEntry, 0, len(page))}
	if end < len(all) {
		out.Next = page[len(page)-1].Name
	}
	var ignored map[string]bool
	if kind == WorktreeRoot && f.ignore != nil {
		var rels []string
		for _, e := range page {
			if e.Kind == domain.EntryDir {
				rels = append(rels, path.Join(q.Dir, e.Name))
			}
		}
		// A listing that cannot read git's rules still lists: nothing is marked.
		ignored, _ = f.ignore.IgnoredPaths(ctx, root.Path, rels)
	}
	for _, e := range page {
		entry := ports.SessionEntry{Name: e.Name, Kind: e.Kind}
		switch e.Kind {
		case domain.EntryDir:
			if kind == WorktreeRoot {
				flag := ignored[path.Join(q.Dir, e.Name)]
				entry.Ignored = &flag
			}
		default:
			// An entry that vanished since the listing keeps its name, without a size.
			if st, err := f.files.Stat(real, e.Name); err == nil {
				if e.Kind == domain.EntrySymlink {
					entry.Target = st.Target
				} else {
					size := st.Size
					entry.Size = &size
				}
			}
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

// SessionFile reads one file as the Code tab does: its content capped at
// domain.MaxCodeFileBytes, with its binary and truncated flags.
func (f *FileSystem) SessionFile(ctx context.Context, sessionID, rootName, file string) (*domain.CodeFile, error) {
	kind, _, err := f.available(ctx, sessionID, rootName)
	if err != nil {
		return nil, err
	}
	if file == "" {
		return nil, refusal("PATH_NOT_FOUND", "the root is not a file")
	}
	if path.Clean(file) != file || path.IsAbs(file) || file == ".." || strings.HasPrefix(file, "../") {
		return nil, refusal("INVALID_INPUT", "the path must be clean and inside the root")
	}
	dir, name := path.Split(file)
	real, err := f.RealPath(ctx, sessionID, kind, strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil, err
	}
	data, size, err := f.files.Read(real, name, domain.MaxCodeFileBytes+1)
	if err != nil {
		return nil, err
	}
	out := &domain.CodeFile{
		Source:   domain.CodeSource{Kind: domain.CodeSourceWorkspace, WorkspaceID: workspaceOf(f.sessions.Get(sessionID))},
		Path:     file,
		Size:     size,
		Language: domain.LanguageOf(file),
		Zones:    []string{},
	}
	if len(data) > domain.MaxCodeFileBytes {
		out.Truncated = true
		data = data[:domain.MaxCodeFileBytes]
		if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i+1]
		}
	}
	if domain.IsBinary(data) {
		out.Binary = true
		return out, nil
	}
	content := string(data)
	out.Content, out.LineCount = &content, domain.CountLines(content)
	return out, nil
}

func workspaceOf(s *domain.Session) *string {
	if s == nil {
		return nil
	}
	return &s.WorkspaceID
}

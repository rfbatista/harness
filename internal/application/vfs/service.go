// Package vfs is the virtual file system of a task session: the files the
// session has, as a tree with two roots. This package holds the resources
// root: a temporary folder with the task's and project's documents,
// comments, resources and designs as ordinary files, kept in sync with
// SQLite, and what each session was last given of each file, its base
// version. Whoever changes one of those, every running resources root that
// should see it is brought up to date (see "Contract: Harness server ↔ Agent
// sessions — virtual shell" §1–3; ADR 0050 renamed the folder).
//
// The package knows nothing of shells: the virtual shell domain depends on
// it, never the reverse.
package vfs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Read-only paths are mode 0444; everything else is 0644.
const (
	modeReadOnly os.FileMode = 0o444
	modeWritable os.FileMode = 0o644
)

// indexFile is the resources root's own index of every other file.
const indexFile = ".index.json"

// Service satisfies the virtual-resources root ports, checked at compile time, and
// hears every change a resources root may have to show.
var (
	_ ports.ResourcesRoots    = (*Service)(nil)
	_ ports.BaseVersions      = (*Service)(nil)
	_ ports.ProjectChangeSink = (*Service)(nil)
	_ ports.TicketAnnouncer   = (*Service)(nil)
)

// Sources is what a resources root is filled from, and Writes, Comments and Designs
// where what agents write in it is stored. Artifacts may be nil: the resources root
// then has no designs; Designs may be nil: designs written in it are then
// not stored. Sessions and Agents name a comment's writer; either may be
// nil.
type Sources struct {
	Tickets   ports.TicketReader
	Documents ports.DocumentReader
	Comments  ports.SessionTaskComments
	Resources ports.TaskResourceFiles
	Artifacts ports.ArtifactReader
	Writes    ports.ShellDocumentWriter
	Designs   ports.ShellDesignWriter
	Sessions  ports.SessionRepository
	Agents    ports.AgentLister
	// Architectures lists the repositories architecture files; nil shows
	// none.
	Architectures ports.ArchitectureFileReader
	// DesignSystems, Projects and Repositories show the project design
	// system, named after the project, to the sessions whose repository
	// uses it; any of them nil shows it to none.
	DesignSystems ports.DesignSystemReader
	Projects      ports.ProjectReader
	Repositories  ports.RepositoryReader
	// Settings holds "Agents move documents to project"; nil reads it as off.
	Settings ports.SettingsReader
}

// Service makes, fills, refreshes and removes sessions' resources roots, and keeps
// each running resources root's base versions in memory.
type Service struct {
	src Sources

	mu    sync.Mutex
	roots map[string]*root // by session id

	// events says when a root comes and goes; nil: nobody listens.
	events *RootEvents

	// rendered is each project's design system files as last made, so a
	// refresh that finds the same version and project name makes none.
	renderMu sync.Mutex
	rendered map[string]renderedDesignSystem
}

// UseRootEvents has the service raise a RootEvent when a session's resources
// root becomes available and when it goes away.
func (s *Service) UseRootEvents(e *RootEvents) { s.events = e }

func (s *Service) rootEvent(sessionID, path string, available bool) {
	s.events.raise(RootEvent{SessionID: sessionID, Kind: ResourcesRoot, Path: path, Available: available})
}

func New(src Sources) *Service {
	return &Service{src: src, roots: map[string]*root{}, rendered: map[string]renderedDesignSystem{}}
}

// resources root is one session's folder and what it was last given of each file.
type root struct {
	dir                         string
	realDir                     string // dir with its symlinks resolved
	sessionID, ticketID, projID string
	repoID                      string // the session's repository, "" for none

	// mu guards the folder and files; a refresh holds it from reading the
	// sources to writing the last file, so refreshes never interleave.
	mu     sync.Mutex
	files  map[string]ports.ShellFile // by path
	closed bool
	// pending is the paths an agent was cleared to change, and since when:
	// a refresh leaves them as they are until the change is committed.
	// skipped says the last refresh left something out for them.
	pending map[string]time.Time
	skipped bool
	// rekick says a refresh is already set for when the holds run out, so
	// refreshes that skip again arm no more timers.
	rekick bool

	// kickMu guards dirty and running: one refresh runs at a time, and a
	// change while it runs makes it go round once more.
	kickMu         sync.Mutex
	dirty, running bool
}

// Open makes the session's resources root in a fresh temp dir and fills it before
// answering its path. A session that had a resources root gets a new one and the old
// one is removed. The resources root hears changes from the moment it is filled.
//
// A scope with no TicketID opens a project-level resources root: project/ and the
// project's tasks under tasks/, no task/ (ADR 0016).
func (s *Service) Open(ctx context.Context, sessionID string, scope ports.ShellScope) (string, error) {
	var tk *domain.Ticket
	projID := scope.ProjectID
	if scope.TicketID != "" {
		var err error
		if tk, err = s.src.Tickets.GetTicket(ctx, scope.TicketID); err != nil {
			return "", err
		}
		projID = tk.ProjectID
	}
	if projID == "" {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "a shell is opened on a task or a project"}
	}
	dir, err := os.MkdirTemp("", "harness-shell-*")
	if err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		realDir = dir
	}
	sh := &root{dir: dir, realDir: realDir, sessionID: sessionID, ticketID: scope.TicketID, projID: projID, repoID: scope.RepositoryID, files: map[string]ports.ShellFile{}}
	// Held until filled: a change heard meanwhile refreshes it after.
	sh.mu.Lock()
	s.mu.Lock()
	old := s.roots[sessionID]
	s.roots[sessionID] = sh
	s.mu.Unlock()

	if err := s.fill(ctx, sh, tk); err != nil {
		sh.closed = true
		sh.mu.Unlock()
		s.mu.Lock()
		if s.roots[sessionID] == sh {
			if old != nil {
				s.roots[sessionID] = old
			} else {
				delete(s.roots, sessionID)
			}
		}
		s.mu.Unlock()
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("fill the resources root: %w", err)
	}
	sh.mu.Unlock()
	if old != nil {
		old.remove()
		s.rootEvent(sessionID, old.realDir, false)
	}
	s.rootEvent(sessionID, realDir, true)
	return dir, nil
}

// fill makes the layout's folders and writes every file; a resources root with no
// task (tk nil) gets no task/ folder. The caller holds sh.mu.
func (s *Service) fill(ctx context.Context, sh *root, tk *domain.Ticket) error {
	dirs := []string{"project/docs", "project/designs", architectureDir, tasksDir}
	if tk != nil {
		dirs = append(dirs, "task/docs", "task/comments", "task/resources", "task/designs")
	}
	for _, d := range dirs {
		if err := os.MkdirAll(sh.abs(d), 0o755); err != nil {
			return err
		}
	}
	b, err := s.contents(ctx, sh, tk)
	if err != nil {
		return err
	}
	return sh.sync(b)
}

// tasksDir is the folder holding the other tasks of the project, one folder
// each named by its id, with its documents under docs.
const tasksDir = "tasks"

// TaskDocsDir is the resources root folder of task id's documents.
func TaskDocsDir(id string) string { return tasksDir + "/" + id + "/docs" }

// syncTasks makes each listed task's folder and removes the folder of a
// task that is gone, once nothing indexed is left in it.
func (sh *root) syncTasks(ids []string) error {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
		if err := os.MkdirAll(sh.abs(TaskDocsDir(id)), 0o755); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(sh.abs(tasksDir))
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if want[ent.Name()] || len(sh.under(tasksDir+"/"+ent.Name(), "")) > 0 {
			continue
		}
		if err := os.RemoveAll(sh.abs(tasksDir + "/" + ent.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Close removes dir, and forgets the session's resources root while dir is still it.
func (s *Service) Close(sessionID, dir string) {
	s.mu.Lock()
	sh := s.roots[sessionID]
	if sh != nil && sh.dir == dir {
		delete(s.roots, sessionID)
	}
	s.mu.Unlock()
	if sh != nil && sh.dir == dir {
		sh.remove()
		s.rootEvent(sessionID, sh.realDir, false)
		return
	}
	_ = os.RemoveAll(dir)
}

// remove closes the resources root, so no refresh writes into it again, and deletes
// its folder.
func (sh *root) remove() {
	sh.mu.Lock()
	sh.closed = true
	sh.mu.Unlock()
	_ = os.RemoveAll(sh.dir)
}

// RootDir is the real path of the session's resources root; false when the
// session has none.
func (s *Service) RootDir(sessionID string) (string, bool) {
	r := s.root(sessionID)
	if r == nil {
		return "", false
	}
	return r.realDir, true
}

// BaseVersion is the file at path as the session last received it.
func (s *Service) BaseVersion(sessionID, path string) (ports.ShellFile, bool) {
	sh := s.root(sessionID)
	if sh == nil {
		return ports.ShellFile{}, false
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	f, ok := sh.files[path]
	return f, ok
}

// ShellFiles is every file of the session's resources root but the index, by path.
func (s *Service) ShellFiles(sessionID string) []ports.ShellFile {
	sh := s.root(sessionID)
	if sh == nil {
		return nil
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.indexRows()
}

// Put writes one file into the session's resources root, atomically, records f as its
// base version and rewrites the index: what the harness does when the file
// changed elsewhere. f.Path is relative to the resources root. A session with no
// resources root is SHELL_NOT_FOUND.
func (s *Service) Put(sessionID string, f ports.ShellFile, content []byte, readOnly bool) error {
	sh := s.root(sessionID)
	if sh == nil {
		return rootNotFound()
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return rootNotFound()
	}
	if err := sh.write(&file{meta: f, content: content, readOnly: readOnly}); err != nil {
		return err
	}
	return sh.writeIndex()
}

// Remove deletes one file from the session's resources root, forgets its base
// version and rewrites the index. A file already gone is not an error.
func (s *Service) Remove(sessionID, path string) error {
	sh := s.root(sessionID)
	if sh == nil {
		return rootNotFound()
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.closed {
		return rootNotFound()
	}
	if err := sh.drop(path); err != nil {
		return err
	}
	return sh.writeIndex()
}

// Subscribe hears the changes announced as domain events: documents' and
// artifacts'. Comments and resources reach the resources roots through
// AnnounceChange.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.DocumentChanged) error {
		s.changed(ev.ProjectID, "")
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.ArtifactChanged) error {
		s.changed(ev.ProjectID, "")
		return nil
	})
	// The README says which rule applies to moving documents to project.
	ports.On(sub, func(_ context.Context, ev domain.SettingsChanged) error {
		if ev.Key == domain.SettingAgentsMoveDocuments {
			s.changedEverywhere()
		}
		return nil
	})
}

// changedEverywhere brings up to date every running resources root.
func (s *Service) changedEverywhere() {
	s.mu.Lock()
	due := make([]*root, 0, len(s.roots))
	for _, sh := range s.roots {
		due = append(due, sh)
	}
	s.mu.Unlock()
	for _, sh := range due {
		s.kick(sh)
	}
}

// agentsMove reads the setting "Agents move documents to project", off while
// unset or unreadable (ADR 0020). It is read at each move and each refresh,
// never kept.
func (s *Service) agentsMove() bool {
	return s.src.Settings != nil && domain.AgentsMoveDocuments(s.src.Settings.Setting(domain.SettingAgentsMoveDocuments))
}

// AnnounceChange hears a task's comments and resources change, as planning
// puts them on the project feed (ports.ProjectChangeSink).
func (s *Service) AnnounceChange(c ports.ProjectChange) {
	switch {
	case c.TicketComment != nil && c.TicketComment.Comment != nil:
		s.changed(c.ProjectID(), c.TicketComment.Comment.TicketID)
	case c.TicketResource != nil && c.TicketResource.Resource != nil:
		s.changed(c.ProjectID(), c.TicketResource.Resource.TicketID)
	case c.ArchitectureChanged != nil, c.DesignSystemSaved != nil:
		s.changed(c.ProjectID(), "")
	}
}

// AnnounceTicket hears a task change, its title, description or status
// among them, or its creation or deletion, as planning announces it: the
// task's resources roots rewrite task/task.json, and every resources root of the project its
// folder under tasks/ (ports.TicketAnnouncer).
func (s *Service) AnnounceTicket(tk *domain.Ticket, deleted bool) {
	if tk != nil {
		s.changed(tk.ProjectID, "")
	}
}

// changed brings up to date every running resources root of the project, or of one
// task of it when ticketID is set. It never waits: each resources root is refreshed
// on its own goroutine, after whatever refresh it is in.
func (s *Service) changed(projectID, ticketID string) {
	if projectID == "" {
		return
	}
	s.mu.Lock()
	var due []*root
	for _, sh := range s.roots {
		if sh.projID == projectID && (ticketID == "" || sh.ticketID == ticketID) {
			due = append(due, sh)
		}
	}
	s.mu.Unlock()
	for _, sh := range due {
		s.kick(sh)
	}
}

// kick refreshes sh, or, when a refresh is running, has it go round once
// more, so the last change is always read after it was stored.
func (s *Service) kick(sh *root) {
	sh.kickMu.Lock()
	sh.dirty = true
	if sh.running {
		sh.kickMu.Unlock()
		return
	}
	sh.running = true
	sh.kickMu.Unlock()
	go func() {
		for {
			sh.kickMu.Lock()
			if !sh.dirty {
				sh.running = false
				sh.kickMu.Unlock()
				return
			}
			sh.dirty = false
			sh.kickMu.Unlock()
			s.refresh(sh)
		}
	}()
}

// refresh reads what the resources root should hold now and rewrites what differs.
func (s *Service) refresh(sh *root) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	s.refreshLocked(sh)
}

// refreshLocked is refresh with sh held. When it leaves out a file held for
// an agent's change, the resources root is refreshed again once the hold runs out.
func (s *Service) refreshLocked(sh *root) {
	if sh.closed {
		return
	}
	ctx := context.Background()
	var tk *domain.Ticket
	if sh.ticketID != "" {
		var err error
		if tk, err = s.src.Tickets.GetTicket(ctx, sh.ticketID); err != nil {
			slog.Warn("refresh a resources root", "session", sh.sessionID, "err", err)
			return
		}
	}
	b, err := s.contents(ctx, sh, tk)
	if err == nil {
		err = sh.sync(b)
	}
	if err != nil {
		slog.Warn("refresh a resources root", "session", sh.sessionID, "err", err)
	}
	if sh.skipped && !sh.rekick {
		sh.rekick = true
		time.AfterFunc(pendingFor, func() {
			sh.mu.Lock()
			sh.rekick = false
			sh.mu.Unlock()
			s.kick(sh)
		})
	}
}

func (s *Service) root(sessionID string) *root {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.roots[sessionID]
}

func rootNotFound() error {
	return &domain.StructuredError{Code: "SHELL_NOT_FOUND", Message: "the session has no virtual shell"}
}

// sync makes the resources root hold exactly b's files: each is placed (a file the
// resources root has keeps its path), written when the resources root lacks that version,
// atomically, and recorded as the session's base version; a file the resources root
// should no longer hold is removed; the index is rewritten when anything
// changed. The caller holds sh.mu.
func (sh *root) sync(b *builder) error {
	prev := map[string]string{}
	for p, f := range sh.files {
		if f.ID != "" {
			prev[fileKey(f)] = p
		}
	}
	b.place(prev, sh.held)
	if err := sh.syncTasks(b.tasks); err != nil {
		return err
	}

	keep := map[string]bool{}
	changed := false
	sh.skipped = false
	put := func(f *file) error {
		if cur, ok := sh.files[f.meta.Path]; ok && current(cur, f.meta) {
			keep[f.meta.Path] = true
			return nil
		}
		if sh.held(f.meta.Path) {
			keep[f.meta.Path], sh.skipped = true, true
			return nil
		}
		if f.load != nil {
			content, err := f.load()
			if err != nil {
				slog.Warn("leave a file out of a resources root", "path", f.meta.Path, "id", f.meta.ID, "err", err)
				f.dropped = true
				return nil
			}
			f.content = content
			if f.meta.Version == "" {
				f.meta.Version = Version(content)
			}
		}
		if err := sh.write(f); err != nil {
			return err
		}
		keep[f.meta.Path], changed = true, true
		return nil
	}
	for _, f := range b.files {
		if f.generate == nil {
			if err := put(f); err != nil {
				return err
			}
		}
	}
	for _, f := range b.files {
		if f.generate != nil {
			f.content = f.generate()
			f.meta.Version = Version(f.content)
			if err := put(f); err != nil {
				return err
			}
		}
	}
	for p := range sh.files {
		if !keep[p] && sh.held(p) {
			sh.skipped = true
			continue
		}
		if !keep[p] {
			if err := sh.drop(p); err != nil {
				return err
			}
			changed = true
		}
	}
	if !b.designSystem {
		// Left empty by an opt-out, the folder goes too: it is absent, not empty.
		_ = os.Remove(sh.abs(designSystemDir))
	}
	if !changed {
		return nil
	}
	return sh.writeIndex()
}

// current says the resources root's row cur already holds want: the same thing at
// the same version, or, while want's version is not known before reading
// its bytes, last changed at the same time.
func current(cur, want ports.ShellFile) bool {
	if cur.Kind != want.Kind || cur.ID != want.ID || cur.Scope != want.Scope || cur.Title != want.Title {
		return false
	}
	if want.Version == "" {
		return cur.UpdatedAt.Equal(want.UpdatedAt)
	}
	return cur.Version == want.Version
}

func (sh *root) abs(p string) string { return filepath.Join(sh.dir, filepath.FromSlash(p)) }

// write puts f on disk through a temp file and a rename, so a reader never
// sees half of it, and records its row.
func (sh *root) write(f *file) error {
	if err := writeAtomic(sh.abs(f.meta.Path), f.content, f.readOnly); err != nil {
		return err
	}
	if f.meta.Path != indexFile {
		sh.files[f.meta.Path] = f.meta
	}
	return nil
}

// drop deletes the file at p and forgets it; one already gone is fine.
func (sh *root) drop(p string) error {
	if err := os.Remove(sh.abs(p)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(sh.files, p)
	return nil
}

func (sh *root) indexRows() []ports.ShellFile {
	rows := make([]ports.ShellFile, 0, len(sh.files))
	for _, f := range sh.files {
		rows = append(rows, f)
	}
	slices.SortFunc(rows, func(a, b ports.ShellFile) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
	return rows
}

func (sh *root) writeIndex() error {
	b, err := json.MarshalIndent(sh.indexRows(), "", "  ")
	if err != nil {
		return err
	}
	return sh.write(&file{meta: ports.ShellFile{Path: indexFile}, content: append(b, '\n'), readOnly: true})
}

func writeAtomic(name string, content []byte, readOnly bool) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".harness-tmp-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	mode := modeWritable
	if readOnly {
		mode = modeReadOnly
	}
	if err := firstErr(werr, cerr, os.Chmod(tmp.Name(), mode)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), name); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// cleanRel is p as a path inside the resources root folder it goes in: slash
// separated, never absolute, never climbing out.
func cleanRel(p string) string {
	c := path.Clean("/" + filepath.ToSlash(p))
	return c[1:]
}

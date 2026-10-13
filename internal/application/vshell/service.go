// Package vshell gives task sessions their virtual shell: a temporary folder
// holding their task's and project's documents, comments, resources and
// designs as files, and what each session was last given of each file, its
// base version (see "Contract: Harness server ↔ Agent sessions — virtual
// shell" §1–2).
package vshell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Read-only paths are mode 0444; everything else is 0644.
const (
	modeReadOnly os.FileMode = 0o444
	modeWritable os.FileMode = 0o644
)

// indexFile is the shell's own index of every other file.
const indexFile = ".index.json"

// Service satisfies the virtual-shell ports, checked at compile time.
var (
	_ ports.VirtualShells     = (*Service)(nil)
	_ ports.ShellBaseVersions = (*Service)(nil)
)

// Sources is what a shell is filled from. Artifacts may be nil: the shell
// then has no designs.
type Sources struct {
	Tickets   ports.TicketReader
	Documents ports.DocumentReader
	Comments  ports.TaskCommentReader
	Resources ports.TaskResourceFiles
	Artifacts ports.ArtifactReader
}

// Service makes, fills and removes sessions' shells, and keeps each running
// shell's base versions in memory.
type Service struct {
	src Sources

	mu     sync.Mutex
	shells map[string]*shell // by session id
}

func New(src Sources) *Service {
	return &Service{src: src, shells: map[string]*shell{}}
}

// shell is one session's folder and what it was last given of each file.
type shell struct {
	dir string

	mu    sync.Mutex
	files map[string]ports.ShellFile // by path
}

// Open makes the session's shell in a fresh temp dir and fills it before
// answering its path. A session that had a shell gets a new one and the old
// one is removed.
func (s *Service) Open(ctx context.Context, sessionID, ticketID string) (string, error) {
	tk, err := s.src.Tickets.GetTicket(ctx, ticketID)
	if err != nil {
		return "", err
	}
	files, err := s.contents(ctx, sessionID, tk)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "harness-shell-*")
	if err != nil {
		return "", err
	}
	sh := &shell{dir: dir, files: map[string]ports.ShellFile{}}
	if err := sh.fill(files); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("fill the virtual shell: %w", err)
	}

	s.mu.Lock()
	old := s.shells[sessionID]
	s.shells[sessionID] = sh
	s.mu.Unlock()
	if old != nil {
		_ = os.RemoveAll(old.dir)
	}
	return dir, nil
}

// Close removes dir, and forgets the session's shell while dir is still it.
func (s *Service) Close(sessionID, dir string) {
	s.mu.Lock()
	if sh := s.shells[sessionID]; sh != nil && sh.dir == dir {
		delete(s.shells, sessionID)
	}
	s.mu.Unlock()
	_ = os.RemoveAll(dir)
}

// BaseVersion is the file at path as the session last received it.
func (s *Service) BaseVersion(sessionID, path string) (ports.ShellFile, bool) {
	sh := s.shell(sessionID)
	if sh == nil {
		return ports.ShellFile{}, false
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	f, ok := sh.files[path]
	return f, ok
}

// ShellFiles is every file of the session's shell but the index, by path.
func (s *Service) ShellFiles(sessionID string) []ports.ShellFile {
	sh := s.shell(sessionID)
	if sh == nil {
		return nil
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.indexRows()
}

// Put writes one file into the session's shell, atomically, records f as its
// base version and rewrites the index: what the harness does when the file
// changed elsewhere. f.Path is relative to the shell. A session with no
// shell is SHELL_NOT_FOUND.
func (s *Service) Put(sessionID string, f ports.ShellFile, content []byte, readOnly bool) error {
	sh := s.shell(sessionID)
	if sh == nil {
		return shellNotFound()
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if err := sh.write(file{meta: f, content: content, readOnly: readOnly}); err != nil {
		return err
	}
	return sh.writeIndex()
}

// Remove deletes one file from the session's shell, forgets its base
// version and rewrites the index. A file already gone is not an error.
func (s *Service) Remove(sessionID, path string) error {
	sh := s.shell(sessionID)
	if sh == nil {
		return shellNotFound()
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if err := os.Remove(sh.abs(path)); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(sh.files, path)
	return sh.writeIndex()
}

func (s *Service) shell(sessionID string) *shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shells[sessionID]
}

func shellNotFound() error {
	return &domain.StructuredError{Code: "SHELL_NOT_FOUND", Message: "the session has no virtual shell"}
}

// file is one file to write: its index row, its bytes and its mode.
type file struct {
	meta     ports.ShellFile
	content  []byte
	readOnly bool
}

// fill writes files and then the index of them.
func (sh *shell) fill(files []file) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	for _, d := range []string{"task/docs", "task/comments", "task/resources", "task/designs", "project/docs", "project/designs"} {
		if err := os.MkdirAll(sh.abs(d), 0o755); err != nil {
			return err
		}
	}
	for _, f := range files {
		if err := sh.write(f); err != nil {
			return err
		}
	}
	return sh.writeIndex()
}

func (sh *shell) abs(p string) string { return filepath.Join(sh.dir, filepath.FromSlash(p)) }

// write puts f on disk through a temp file and a rename, so a reader never
// sees half of it, and records its row.
func (sh *shell) write(f file) error {
	if err := writeAtomic(sh.abs(f.meta.Path), f.content, f.readOnly); err != nil {
		return err
	}
	if f.meta.Path != indexFile {
		sh.files[f.meta.Path] = f.meta
	}
	return nil
}

func (sh *shell) indexRows() []ports.ShellFile {
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

func (sh *shell) writeIndex() error {
	b, err := json.MarshalIndent(sh.indexRows(), "", "  ")
	if err != nil {
		return err
	}
	return sh.write(file{meta: ports.ShellFile{Path: indexFile}, content: append(b, '\n'), readOnly: true})
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

// cleanRel is p as a path inside the shell folder it goes in: slash
// separated, never absolute, never climbing out.
func cleanRel(p string) string {
	c := path.Clean("/" + filepath.ToSlash(p))
	return c[1:]
}

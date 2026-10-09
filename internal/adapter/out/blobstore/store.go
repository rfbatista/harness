// Package blobstore keeps the bytes of the resources people add to tasks, as
// plain files in one directory under the harness's data directory: one file
// per resource, named by its id. Content is staged in a temporary file and
// renamed into place once its record exists, so a half-written upload never
// has a path.
package blobstore

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.ResourceBlobStore = (*Store)(nil)

// headLen is how much of the content Head keeps, to infer a type from.
const headLen = 512

// Store is a ports.ResourceBlobStore over a directory, created on first use.
type Store struct{ dir string }

// stagingPrefix names content still being received.
const stagingPrefix = ".staging-"

// New returns a store over dir. Staging files left there by a run that
// stopped mid-upload are removed: at startup nothing is being received.
func New(dir string) *Store {
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), stagingPrefix) {
				os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return &Store{dir: dir}
}

func (s *Store) Stage(r io.Reader, max int64) (ports.StagedBlob, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(s.dir, stagingPrefix+"*")
	if err != nil {
		return nil, err
	}
	discard := func() { f.Close(); os.Remove(f.Name()) }
	head := &headWriter{}
	n, err := io.Copy(io.MultiWriter(f, head), io.LimitReader(r, max+1))
	if err != nil {
		discard()
		return nil, err
	}
	if n > max {
		discard()
		return nil, ports.ErrBlobTooLarge
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	return &staged{store: s, tmp: f.Name(), size: n, head: head.b}, nil
}

func (s *Store) path(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("blobstore: invalid id %q", id)
	}
	return filepath.Join(s.dir, id), nil
}

func (s *Store) Open(id string) (*ports.Blob, error) {
	p, err := s.path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &ports.Blob{Content: f, ModTime: st.ModTime()}, nil
}

// Path is where id's bytes live, absolute when the directory is.
func (s *Store) Path(id string) string {
	p, err := s.path(id)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func (s *Store) Delete(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type staged struct {
	store *Store
	tmp   string
	size  int64
	head  []byte
}

func (b *staged) Size() int64  { return b.size }
func (b *staged) Head() []byte { return b.head }
func (b *staged) Discard()     { os.Remove(b.tmp) }

func (b *staged) Commit(id string) error {
	p, err := b.store.path(id)
	if err != nil {
		b.Discard()
		return err
	}
	if err := os.Rename(b.tmp, p); err != nil {
		b.Discard()
		return err
	}
	return nil
}

// headWriter keeps the first headLen bytes written to it.
type headWriter struct{ b []byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := headLen - len(h.b); room > 0 {
		h.b = append(h.b, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

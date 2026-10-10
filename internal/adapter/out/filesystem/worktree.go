package filesystem

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.WorktreeFiles = (*WorktreeFiles)(nil)

// WorktreeFiles reads a worktree's files through os.Root, so no read can
// leave the worktree, and checks every component of a path with lstat, so
// no symlink is followed.
type WorktreeFiles struct{}

// NewWorktreeFiles returns the adapter.
func NewWorktreeFiles() *WorktreeFiles { return &WorktreeFiles{} }

func pathNotFound(p string) error {
	return &domain.StructuredError{Code: "PATH_NOT_FOUND", Message: p + " is not in the worktree"}
}

func linkOutside(p string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: p + " is a symlink out of the worktree"}
}

// Exists reports whether root is a directory on disk.
func (WorktreeFiles) Exists(root string) bool {
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

// Stat describes path ("" the root) without following it. A path through a
// symlink is PATH_NOT_FOUND, or INVALID_INPUT when the link leaves root.
func (w WorktreeFiles) Stat(root, p string) (ports.FileStat, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return ports.FileStat{}, missing(root, err)
	}
	defer r.Close()
	return stat(r, p)
}

// missing maps a worktree that cannot be opened.
func missing(root string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return &domain.StructuredError{Code: "WORKSPACE_MISSING", Message: "the worktree " + root + " is no longer on disk"}
	}
	return err
}

func stat(r *os.Root, p string) (ports.FileStat, error) {
	if p == "" {
		return ports.FileStat{Kind: domain.EntryDir}, nil
	}
	segs := strings.Split(p, "/")
	for i := range segs {
		sub := strings.Join(segs[:i+1], "/")
		info, err := r.Lstat(sub)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				return ports.FileStat{}, pathNotFound(p)
			}
			return ports.FileStat{}, err
		}
		last := i == len(segs)-1
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := r.Readlink(sub)
			if err != nil {
				return ports.FileStat{}, err
			}
			escapes := domain.SymlinkEscapes(sub, target)
			if !last {
				if escapes {
					return ports.FileStat{}, linkOutside(p)
				}
				return ports.FileStat{}, pathNotFound(p)
			}
			return ports.FileStat{Kind: domain.EntrySymlink, Target: target, Escapes: escapes}, nil
		case info.IsDir():
			if last {
				return ports.FileStat{Kind: domain.EntryDir}, nil
			}
		case !last:
			return ports.FileStat{}, pathNotFound(p)
		case info.Mode().IsRegular():
			return ports.FileStat{Kind: domain.EntryFile, Size: info.Size()}, nil
		default:
			return ports.FileStat{Size: info.Size()}, nil
		}
	}
	return ports.FileStat{}, pathNotFound(p)
}

// open opens the regular file path for reading, refusing anything else.
func open(root, p string) (*os.File, *os.Root, ports.FileStat, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, ports.FileStat{}, missing(root, err)
	}
	st, err := stat(r, p)
	if err == nil && st.Kind != domain.EntryFile {
		if st.Kind == domain.EntrySymlink && st.Escapes {
			err = linkOutside(p)
		} else {
			err = pathNotFound(p)
		}
	}
	if err != nil {
		r.Close()
		return nil, nil, st, err
	}
	// O_NOFOLLOW: a file swapped for a link since the lstat is not followed.
	f, err := r.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		r.Close()
		return nil, nil, st, pathNotFound(p)
	}
	return f, r, st, nil
}

// Read reads at most max bytes of the regular file path, and its size.
func (WorktreeFiles) Read(root, p string, max int64) ([]byte, int64, error) {
	f, r, st, err := open(root, p)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil, 0, err
	}
	return data, st.Size, nil
}

// Lines counts the lines of the regular file path in one pass, in bounded
// memory, and tells whether it is binary (domain.IsBinary over the whole
// file).
func (WorktreeFiles) Lines(root, p string) (int, bool, error) {
	f, r, _, err := open(root, p)
	if err != nil {
		return 0, false, err
	}
	defer r.Close()
	defer f.Close()
	buf := make([]byte, 64<<10)
	var carry []byte // an incomplete rune at the end of the last chunk
	lines, read, binary := 0, 0, false
	var last byte
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if read < 8000 && bytes.IndexByte(chunk[:min(n, 8000-read)], 0) >= 0 {
				binary = true
			}
			read += n
			lines += bytes.Count(chunk, []byte{'\n'})
			last = chunk[n-1]
			if !binary {
				text := append(carry, chunk...)
				cut := len(text)
				for j := len(text) - 1; j >= 0 && j >= len(text)-utf8.UTFMax; j-- {
					if utf8.RuneStart(text[j]) {
						if !utf8.FullRune(text[j:]) {
							cut = j
						}
						break
					}
				}
				if !utf8.Valid(text[:cut]) {
					binary = true
				}
				carry = append(carry[:0], text[cut:]...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, false, err
		}
	}
	if len(carry) > 0 && !utf8.Valid(carry) {
		binary = true
	}
	if read > 0 && last != '\n' {
		lines++
	}
	return lines, binary, nil
}

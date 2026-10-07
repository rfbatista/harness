package artifacts

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

// snapshotFile is one file a snapshot copies: where it is read from, and
// where it lands relative to the snapshot root.
type snapshotFile struct {
	src, rel string
	size     int64
}

// copyArtifactDir copies the directory of the artifact at artifactPath
// (worktree-relative), recursively, into dest: the file plus everything the
// view route could serve next to or under it. The worktree's confinement
// rules hold: nothing under .git (in any case), and a symlinked file is
// copied only when its target stays in the worktree. Symlinked directories
// are not followed. The size cap applies to the copy as a whole and is
// checked before anything is written. dest appears whole or not at all.
func copyArtifactDir(worktree, artifactPath, dest string, max int64) error {
	if _, _, err := resolveInWorktree(worktree, filepath.FromSlash(artifactPath)); err != nil {
		return err
	}
	dir, _, err := resolveInWorktree(worktree, filepath.FromSlash(path.Dir(artifactPath)))
	if err != nil {
		return err
	}
	var files []snapshotFile
	var total int64
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.EqualFold(d.Name(), ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		src := p
		switch {
		case d.Type().IsRegular():
		case d.Type()&fs.ModeSymlink != 0:
			real, _, rerr := resolveInWorktree(worktree, p)
			if rerr != nil {
				return nil
			}
			if info, serr := os.Stat(real); serr != nil || !info.Mode().IsRegular() {
				return nil
			}
			src = real
		default:
			return nil
		}
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files = append(files, snapshotFile{src: src, rel: rel, size: info.Size()})
		total += info.Size()
		return nil
	})
	if walkErr != nil {
		return notFound(path.Dir(artifactPath) + " cannot be read: " + walkErr.Error())
	}
	if total > max {
		return &domain.StructuredError{Code: "ARTIFACT_TOO_LARGE",
			Message: fmt.Sprintf("the artifact's directory %s holds %s; the server keeps copies up to %s", path.Dir(artifactPath), humanBytes(total), humanBytes(max))}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp-" + randSuffix()
	if err := writeSnapshot(tmp, files); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

func writeSnapshot(root string, files []snapshotFile) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		if err := copyFile(f.src, filepath.Join(root, f.rel)); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src to dst, keeping its mtime so Last-Modified still
// says when the file was written.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func randSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

package capabilities

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxUnpackedBytes bounds what an archive may unpack to.
const maxUnpackedBytes = 2 << 30

// unpack extracts the archive at src (a .tar.gz or a zip, told apart by its
// first bytes) into dest and answers entry's path relative to dest. With
// only, it extracts just entry, at its base name. Symbolic links and other
// special entries are skipped; an entry that would land outside dest fails
// the whole unpack.
func unpack(src, dest, entry string, only bool) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	magic := make([]byte, 4)
	if _, err := io.ReadFull(f, magic); err != nil {
		return "", errors.New("it is not an archive")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	x := &extractor{dest: dest, entry: entry, only: only, left: maxUnpackedBytes}
	switch {
	case bytes.Equal(magic[:2], []byte{0x1f, 0x8b}):
		err = x.tarGz(f)
	case bytes.Equal(magic, []byte("PK\x03\x04")):
		err = x.zip(f)
	default:
		err = errors.New("it is neither a .tar.gz nor a zip")
	}
	if err != nil {
		return "", err
	}
	rel := entry
	if only {
		rel = path.Base(entry)
	}
	if info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("it has no file %s", entry)
	}
	return rel, nil
}

type extractor struct {
	dest, entry string
	only        bool
	left        int64
}

func (x *extractor) tarGz(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = x.dir(h.Name)
		case tar.TypeReg:
			err = x.file(h.Name, h.FileInfo().Mode(), tr)
		}
		if err != nil {
			return err
		}
	}
}

func (x *extractor) zip(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	for _, zf := range zr.File {
		mode := zf.Mode()
		switch {
		case mode.IsDir():
			err = x.dir(zf.Name)
		case mode.IsRegular():
			var rc io.ReadCloser
			if rc, err = zf.Open(); err == nil {
				err = x.file(zf.Name, mode, rc)
				rc.Close()
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// target is where name unpacks to, or "" when only skips it.
func (x *extractor) target(name string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("entry %s points outside the archive", name)
	}
	if x.only {
		if clean != x.entry {
			return "", nil
		}
		clean = path.Base(clean)
	}
	if clean == "." {
		return "", nil
	}
	return filepath.Join(x.dest, filepath.FromSlash(clean)), nil
}

func (x *extractor) dir(name string) error {
	if x.only {
		return nil
	}
	to, err := x.target(name)
	if err != nil || to == "" {
		return err
	}
	return os.MkdirAll(to, 0o755)
}

func (x *extractor) file(name string, mode os.FileMode, r io.Reader) error {
	to, err := x.target(name)
	if err != nil || to == "" {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, x.left+1))
	x.left -= n
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && x.left < 0 {
		err = errors.New("it unpacks to more than 2 GB")
	}
	return err
}

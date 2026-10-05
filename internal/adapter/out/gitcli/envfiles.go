package gitcli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.EnvFileIO = (*EnvFiles)(nil)

// EnvFiles reads env files from checkouts and writes them into worktrees,
// keeping git from tracking them.
type EnvFiles struct{}

// NewEnvFiles returns the adapter.
func NewEnvFiles() *EnvFiles { return &EnvFiles{} }

// Read returns the file at rel inside root, or ENV_FILE_NOT_FOUND.
func (EnvFiles) Read(root, rel string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &domain.StructuredError{Code: "ENV_FILE_NOT_FOUND", Message: rel + " does not exist in " + root}
	}
	return b, err
}

// Write writes each file under root with mode 0600, creating its directory,
// then makes sure git ignores it: a path the repository does not already
// ignore is added to its info/exclude (shared by all its worktrees, and never
// committed), so the secrets cannot end up in a session's commits.
func (EnvFiles) Write(root string, files []*domain.EnvFile) error {
	for _, f := range files {
		target := filepath.Join(root, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(f.Content), 0o600); err != nil {
			return err
		}
		if err := ensureIgnored(root, f.Path); err != nil {
			return err
		}
	}
	return nil
}

// ensureIgnored adds rel to the repository's info/exclude unless git already
// ignores it.
func ensureIgnored(root, rel string) error {
	if exec.Command("git", "-C", root, "check-ignore", "-q", "--", rel).Run() == nil {
		return nil // already ignored
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return err
	}
	exclude := filepath.Join(string(bytes.TrimSpace(out)), "info", "exclude")
	line := "/" + strings.TrimPrefix(rel, "/")
	current, err := os.ReadFile(exclude)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(string(current), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return err
	}
	fh, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	prefix := ""
	if len(current) > 0 && !bytes.HasSuffix(current, []byte("\n")) {
		prefix = "\n"
	}
	_, err = fh.WriteString(prefix + "# written by the harness: a session's env file\n" + line + "\n")
	return err
}

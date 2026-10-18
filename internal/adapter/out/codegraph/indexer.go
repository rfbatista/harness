// Package codegraph runs the installed codegraph to build repository
// indexes (ADR 0014).
package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RepositoryIndexer = Indexer{}

// database is the index's SQLite file inside .codegraph/; its WAL sits
// beside it with a -wal suffix.
const database = "codegraph.db"

// Indexer builds repository indexes with the codegraph executable it is
// handed.
type Indexer struct{}

// NewIndexer returns the adapter.
func NewIndexer() Indexer { return Indexer{} }

// BuildIndex excludes /.codegraph/ from git first, so the checkout never
// shows it as a change, then runs `codegraph init --yes` where there is no
// index yet and `codegraph index --quiet` (a full rebuild) where there is,
// in checkout, with codegraph's telemetry off and its file watching forced
// on: init installs git hooks in the checkout when it cannot watch (a WSL
// /mnt drive, or CODEGRAPH_NO_WATCH set), and the harness never writes the
// checkout's hooks.
func (Indexer) BuildIndex(ctx context.Context, codegraph, checkout string) error {
	if err := gitcli.Exclude(checkout, domain.RepositoryIndexDir+"/", "the codegraph repository index"); err != nil {
		return fmt.Errorf("keeping %s/ out of git: %w", domain.RepositoryIndexDir, err)
	}
	args := []string{"init", "--yes", checkout}
	if _, err := os.Stat(filepath.Join(checkout, domain.RepositoryIndexDir, database)); err == nil {
		args = []string{"index", "--quiet", checkout}
	}
	cmd := exec.CommandContext(ctx, codegraph, args...)
	cmd.Dir = checkout
	cmd.Env = buildEnv(os.Environ())
	var out tail
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("codegraph %s: %w: %s", args[0], err, msg)
		}
		return fmt.Errorf("codegraph %s: %w", args[0], err)
	}
	return nil
}

// buildEnv is env for a build: without CODEGRAPH_NO_WATCH, with watching
// forced and telemetry off whatever env said.
func buildEnv(env []string) []string {
	forced := []string{"CODEGRAPH_FORCE_WATCH=1", "CODEGRAPH_TELEMETRY=0", "DO_NOT_TRACK=1"}
	out := slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return name == "CODEGRAPH_NO_WATCH" || slices.ContainsFunc(forced, func(f string) bool { return strings.HasPrefix(f, name+"=") })
	})
	return append(out, forced...)
}

// IndexBuiltAt is when codegraph last wrote checkout's index database (or
// its WAL). Sessions work on copies in their worktrees, so in the checkout
// that is the last build: the harness's, or one a person ran there.
func (Indexer) IndexBuiltAt(checkout string) (time.Time, bool) {
	db := filepath.Join(checkout, domain.RepositoryIndexDir, database)
	info, err := os.Stat(db)
	if err != nil {
		return time.Time{}, false
	}
	at := info.ModTime()
	if wal, err := os.Stat(db + "-wal"); err == nil && wal.ModTime().After(at) {
		at = wal.ModTime()
	}
	return at, true
}

// tail keeps the last 4 KB written to it: enough of codegraph's output to
// say why a build failed.
type tail struct{ buf bytes.Buffer }

const tailBytes = 4 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if extra := t.buf.Len() - tailBytes; extra > 0 {
		t.buf.Next(extra)
	}
	return len(p), nil
}

func (t *tail) String() string { return t.buf.String() }

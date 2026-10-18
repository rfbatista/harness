package orchestration

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// codegraphServerName names codegraph's MCP server in a session's config,
// and so its tools: mcp__codegraph__*.
const codegraphServerName = "codegraph"

// wireCodegraph gives the session codegraph's MCP server, with its tools
// allowed and its telemetry off, and starts its worktree from a copy of the
// repository index (ADR 0014), unless a build is writing that index now.
// The server has no working dir of its own: claude spawns it in the
// worktree, where claude runs.
func (s *Service) wireCodegraph(w saverSession) error {
	w.cfg.MCPServers = append(w.cfg.MCPServers, llmkit.MCPServerSpec{
		Name:      codegraphServerName,
		Transport: "stdio",
		Command:   w.installation.Path,
		Args:      []string{"serve", "--mcp"},
		Env:       map[string]string{"CODEGRAPH_TELEMETRY": "0", "DO_NOT_TRACK": "1"},
	})
	// A bare server name allows every tool the server offers.
	w.cfg.AllowedTools = append(w.cfg.AllowedTools, "mcp__"+codegraphServerName)
	withEnv(w.cfg, "CODEGRAPH_TELEMETRY=0", "DO_NOT_TRACK=1")
	switch {
	case w.resumed:
	case s.IndexBuilds != nil && s.IndexBuilds.IndexBuilding(w.checkout):
		slog.Info("repository index building; the session starts without it", "worktree", w.worktree)
	default:
		copyRepositoryIndex(w.checkout, w.worktree)
	}
	return nil
}

// copyRepositoryIndex copies the repository index in checkout into
// worktree. A missing index is skipped and a failed copy is left out: the
// session starts anyway and codegraph answers without one.
func copyRepositoryIndex(checkout, worktree string) {
	if checkout == "" {
		return
	}
	src := filepath.Join(checkout, domain.RepositoryIndexDir)
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return
	}
	dst := filepath.Join(worktree, domain.RepositoryIndexDir)
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		slog.Warn("repository index not copied; the session starts without it", "worktree", worktree, "err", err)
		_ = os.RemoveAll(dst)
	}
}

// Package app is the composition root for the operators-mcp server. It wires
// the dependency graph with uber-go/fx: persistence, the catalog bounded
// contexts, the Genkit execution layer, the tooling registry, and the inbound
// HTTP and MCP servers. Each concern is an fx.Module so it can be tested in
// isolation, and process lifecycle (listen/shutdown, DB close) lives in fx
// lifecycle hooks rather than ad-hoc goroutines in main().
package app

import (
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"operators-mcp/internal/adapter/out/gitcli"
)

// Config holds the process-level settings resolved from flags in main().
// It is supplied into the fx graph as a value and consumed by the providers
// and lifecycle hooks that need it.
type Config struct {
	// HTTPAddr is the listen address for the UI + JSON API server.
	HTTPAddr string
	// MCPAddr is the listen address for the MCP streamable HTTP server the IDE connects to.
	MCPAddr string
	// DBPath is the SQLite database path (e.g. "data.db" or ":memory:").
	DBPath string
	// DataDir is the absolute directory the harness keeps its data in: the
	// database by default, and tools/, artifacts/, task-resources/, git/ and
	// secret.key always. cliflags resolves it. Empty: the database's folder,
	// or temporary directories for an in-memory database.
	DataDir string
	// DataDirSource says where DataDir came from, in words, for the Server
	// page: the -data.dir flag, HARNESS_DATA_DIR, the database path, or the
	// default.
	DataDirSource string
	// DBSlowThreshold is how long a database call may take before it is
	// logged as a slow database call. Zero uses the SQLite adapter's default.
	DBSlowThreshold time.Duration
	// SlowRequestThreshold is how long an HTTP request may take before it
	// is logged as a slow request, with its query count. Zero uses the HTTP
	// API adapter's default.
	SlowRequestThreshold time.Duration
	// GitWorktreeDeadline bounds each git run that adds, removes or prunes a
	// worktree; GitReadDeadline each git run that only reads. A git that
	// gives no answer in time is killed and its operation fails. Zero uses
	// the git adapter's defaults. Push and fetch keep their own deadline.
	GitWorktreeDeadline time.Duration
	GitReadDeadline     time.Duration
	// GitBin is the git binary the server runs. New resolves it once (see
	// gitcli.ResolveGit): a value set here wins; empty uses the first git on
	// PATH, or the real git behind macOS's /usr/bin/git shim.
	GitBin string
	// DevMode proxies the designer resource to the Vite dev server instead of the embedded build.
	DevMode bool
	// Root is the default project root for tree and path operations (usually the working directory).
	Root string
	// ClaudeBin is the path to the claude CLI binary (default "claude").
	ClaudeBin string
	// APIToken, when set, is the bearer token every /api request must carry
	// (but /api/health). Empty leaves the API open, which is only safe while
	// HTTPAddr is loopback; the server warns otherwise.
	APIToken string
	// SessionShell is how the interactive sessions the server runs are
	// started: "direct" or "login" (see runtime.Config).
	SessionShell string
	// ShimBinary is the binary task sessions' PATH shims (sed, tee, cp, mv,
	// rm, touch, mkdir) link to: cmd/server sets it to itself, as it runs
	// the shim when invoked under one of those names. Empty: no shims.
	ShimBinary string
	// ClaudeLogStdout tees each session's raw stdout to the terminal, prefixed
	// with the session id. Disable to keep the console quiet.
	ClaudeLogStdout bool
	// ClaudeTextModel is the model the one-shot text-processing adapters
	// (summarize, generate, translate) run on. Empty keeps whatever default the
	// CLI is configured with; set it to pin those utilities to a cheaper model
	// than the one interactive sessions use.
	ClaudeTextModel string
	// ApprovalTimeout is how long a spawned session blocks on one approval or
	// question before the CLI gives up on it. It is the ceiling on how long a
	// user may take to answer, so it is measured in hours, not seconds.
	ApprovalTimeout time.Duration
	// DotfilesAgentsDir is the path to a dotfiles-style agents/ directory
	// (agents.json + skills/ + per-agent .mcp.json). It is only the source of
	// the one-time agent catalog import (ADR 0030): the catalog itself is the
	// "agents" folder of the data dir. Empty: agents/ under Root, when it has
	// an agents.json.
	DotfilesAgentsDir string
	// TokenSaverReleaseBase is where token savers' pinned releases are
	// downloaded from, in place of https://github.com (a mirror, or a test's
	// fake release server). Empty uses GitHub.
	TokenSaverReleaseBase string
	// ManagedClaudeReleaseBase is where the managed claude is downloaded
	// from, in place of https://downloads.claude.ai/claude-code-releases (a
	// mirror, or a test's fake release server). Empty uses the official one.
	ManagedClaudeReleaseBase string
	// ProfilerOff stops the HTTP server serving Go's profiler under
	// /debug/pprof/. Left false, the profiler answers loopback peers only.
	ProfilerOff bool
}

// inMemory reports whether the database lives in memory only.
func (c Config) inMemory() bool { return c.DBPath == "" || c.DBPath == ":memory:" }

// dataRoot is DataDir, or the database's folder when it is unset, always
// absolute; "" when neither names a place on disk.
func (c Config) dataRoot() (string, error) {
	if c.DataDir != "" {
		return filepath.Abs(c.DataDir)
	}
	if c.inMemory() {
		return "", nil
	}
	return filepath.Abs(filepath.Dir(c.DBPath))
}

// loopbackBaseURL is the base URL the spawned claude reaches this process at for
// its per-session MCP endpoints (approval and task); derived from HTTPAddr.
func (c Config) loopbackBaseURL() string {
	addr := c.HTTPAddr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr
}

// llmProxyURL is the LLM proxy every claude process calls through (ADR
// 0006): the server's own loopback address under /llm.
func (c Config) llmProxyURL() string {
	return c.loopbackBaseURL() + "/llm"
}

// New builds the fully wired fx application for the given configuration.
// Call Run on the result to start the servers and block until SIGINT/SIGTERM.
func New(cfg Config) *fx.App {
	cfg.GitBin = gitcli.ResolveGit(cfg.GitBin)
	slog.Info("git", "bin", cfg.GitBin)
	if cfg.DataDir != "" {
		slog.Info("data dir", "dir", cfg.DataDir, "set by", cfg.DataDirSource, "db", cfg.DBPath)
	}
	return fx.New(
		fx.StopTimeout(stopTimeout),
		fx.Supply(cfg),
		PersistenceModule,
		CatalogModule,
		PlanningModule,
		WorkspacesModule,
		ExecutionModule,
		AgentRuntimeModule,
		TaskChannelModule,
		NotificationsModule,
		UsageModule,
		GitCredentialsModule,
		TextProcessingModule,
		ToolingModule,
		TokenSaversModule,
		ServerModule,
		fx.WithLogger(func() fxevent.Logger {
			return &fxevent.SlogLogger{Logger: slog.Default()}
		}),
	)
}

// Validate boots the graph far enough to type-check every provider and detect
// missing dependencies or cycles, without starting any server. Intended for
// tests and CI graph validation.
func Validate(cfg Config) error {
	return fx.ValidateApp(
		fx.Supply(cfg),
		PersistenceModule,
		CatalogModule,
		PlanningModule,
		WorkspacesModule,
		ExecutionModule,
		AgentRuntimeModule,
		TaskChannelModule,
		NotificationsModule,
		UsageModule,
		GitCredentialsModule,
		TextProcessingModule,
		ToolingModule,
		TokenSaversModule,
		ServerModule,
	)
}

// stopTimeout bounds graceful shutdown: servers drain, sessions stop, DB closes.
const stopTimeout = 8 * time.Second

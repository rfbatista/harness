// Package app is the composition root for the operators-mcp server. It wires
// the dependency graph with uber-go/fx: persistence, the catalog bounded
// contexts, the Genkit execution layer, the tooling registry, and the inbound
// HTTP and MCP servers. Each concern is an fx.Module so it can be tested in
// isolation, and process lifecycle (listen/shutdown, DB close) lives in fx
// lifecycle hooks rather than ad-hoc goroutines in main().
package app

import (
	"log/slog"
	"strings"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
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
	// DevMode proxies the designer resource to the Vite dev server instead of the embedded build.
	DevMode bool
	// Root is the default project root for tree and path operations (usually the working directory).
	Root string
	// ClaudeBin is the path to the claude CLI binary (default "claude").
	ClaudeBin string
	// SessionShell is how the interactive sessions the server runs are
	// started: "direct" or "login" (see runtime.Config).
	SessionShell string
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
	// (agents.json + skills/). When set, Agent/Skill/MCPServer data from it is
	// read live on every request and merged alongside the database-backed
	// catalog (config wins on a name collision) — see
	// internal/adapter/out/configrepo. Empty disables this entirely; the
	// catalog is then exactly what's in the database, as before.
	DotfilesAgentsDir string
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

// New builds the fully wired fx application for the given configuration.
// Call Run on the result to start the servers and block until SIGINT/SIGTERM.
func New(cfg Config) *fx.App {
	return fx.New(
		fx.StopTimeout(stopTimeout),
		fx.Supply(cfg),
		PersistenceModule,
		CatalogModule,
		PlanningModule,
		WorkspacesModule,
		ExecutionModule,
		AgentRuntimeModule,
		TextProcessingModule,
		ToolingModule,
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
		TextProcessingModule,
		ToolingModule,
		ServerModule,
	)
}

// stopTimeout bounds graceful shutdown: servers drain, sessions stop, DB closes.
const stopTimeout = 8 * time.Second

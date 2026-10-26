// Package cliflags resolves the process configuration shared by every binary
// that boots the application graph (the server and the TUI): the same flags,
// the same environment fallbacks, the same defaults. Each main adds its own
// extra flags to the FlagSet before calling Parse.
package cliflags

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app"
)

// Getenv looks up one environment variable; os.Getenv in production, a map in tests.
type Getenv func(key string) string

// Parse registers the shared flags on fs, parses args, and returns the Config.
// Precedence is flag > environment > default. Unparseable environment values
// fall back to the default rather than failing: a typo must not silently
// shorten how long a session waits for the user.
func Parse(fs *flag.FlagSet, args []string, getenv Getenv) (app.Config, error) {
	httpAddr := fs.String("http.addr", envOr(getenv, "HTTP_ADDR", ":8080"), "HTTP server listen address (UI and API)")
	mcpAddr := fs.String("mcp.addr", envOr(getenv, "MCP_ADDR", ":8081"), "MCP server listen address (IDE connects here)")
	dataDir := fs.String("data.dir", envOr(getenv, DataDirEnv, ""), "where the harness keeps its data: the database by default, tools/, artifacts/, task-resources/, git/ and secret.key; empty uses the database's folder when -db is set, else ~/.harness")
	dbPath := fs.String("db", envOr(getenv, "DB_PATH", ""), "SQLite database path (e.g. data.db or :memory:); empty uses data.db in the data dir")
	dbSlowThreshold := fs.Duration("db.slow-threshold", envDuration(getenv, "DB_SLOW_THRESHOLD", sqlite.DefaultSlowThreshold), "log every database call slower than this as a slow database call")
	slowRequestThreshold := fs.Duration("http.slow-request-threshold", envDuration(getenv, "HTTP_SLOW_REQUEST_THRESHOLD", httpapi.DefaultSlowRequestThreshold), "log every HTTP request slower than this as a slow request, with its query count")
	gitWorktreeDeadline := fs.Duration("git.worktree-deadline", envDuration(getenv, "CODING_POOL_GIT_WORKTREE_DEADLINE", gitcli.DefaultWorktreeDeadline), "how long one git worktree add, remove or prune may run before it is killed and its operation fails")
	gitReadDeadline := fs.Duration("git.read-deadline", envDuration(getenv, "CODING_POOL_GIT_READ_DEADLINE", gitcli.DefaultReadDeadline), "how long one read-only git run may take before it is killed and its operation fails")
	gitBin := fs.String("git.bin", envOr(getenv, "CODING_POOL_GIT_BIN", ""), "the git binary to run; empty uses the first git on PATH, or the real git behind macOS's /usr/bin/git shim")
	devMode := fs.Bool("dev", envBool(getenv, "DEV_MODE", false), "proxy ui://designer to the Vite dev server (run 'make web-dev' separately)")
	claudeBin := fs.String("claude.bin", envOr(getenv, "CLAUDE_BIN", "claude"), "path to the claude CLI binary")
	apiToken := fs.String("api.token", envOr(getenv, "CODING_POOL_TOKEN", ""), "bearer token required on /api (but /api/health); empty leaves the API open")
	sessionShell := fs.String("session.shell", envOr(getenv, "CODING_POOL_SHELL", "direct"), "how interactive sessions the server runs are started: direct, or login to run them through the login shell")
	claudeLogStdout := fs.Bool("claude.log-stdout", envBool(getenv, "CLAUDE_LOG_STDOUT", true), "tee each session's raw stdout to the terminal")
	claudeTextModel := fs.String("claude.text-model", envOr(getenv, "CLAUDE_TEXT_MODEL", ""), "model for the one-shot text utilities (summarize/generate/translate); empty uses the CLI default")
	approvalTimeout := fs.Duration("approval.timeout", envDuration(getenv, "APPROVAL_TIMEOUT", 24*time.Hour), "how long a session waits for the user to answer an approval or question")
	dotfilesAgentsDir := fs.String("dotfiles.agents-dir", envOr(getenv, "DOTFILES_AGENTS_DIR", ""), "path to a dotfiles-style agents/ directory (agents.json + skills/) the one-time agent catalog import reads; the catalog itself lives in the data dir. Empty uses agents/ under the project root when it has an agents.json")
	tokenSaverReleaseBase := fs.String("token-savers.release-base", envOr(getenv, "TOKEN_SAVER_RELEASE_BASE", ""), "where token savers' pinned releases are downloaded from instead of https://github.com (<base>/<owner>/<repo>/releases/download/<tag>/<asset>)")
	profiler := fs.Bool("pprof", envBool(getenv, "PPROF", true), "serve Go's profiler, goroutine dump included, under /debug/pprof/ to loopback peers only")

	if err := fs.Parse(args); err != nil {
		return app.Config{}, err
	}

	root := envOr(getenv, "PROJECT_ROOT", "")
	if root == "" {
		root, _ = os.Getwd()
	}

	data, err := resolveDataDir(fs, getenv, *dataDir, *dbPath)
	if err != nil {
		return app.Config{}, err
	}

	return app.Config{
		HTTPAddr:            *httpAddr,
		MCPAddr:             *mcpAddr,
		DBPath:              data.db,
		DataDir:             data.dir,
		DataDirSource:       data.source,
		DBSlowThreshold:     *dbSlowThreshold,
		GitWorktreeDeadline: *gitWorktreeDeadline,
		GitReadDeadline:     *gitReadDeadline,
		GitBin:              *gitBin,
		DevMode:             *devMode,
		Root:                root,
		ClaudeBin:           *claudeBin,
		SessionShell:        *sessionShell,
		APIToken:            *apiToken,
		ClaudeLogStdout:     *claudeLogStdout,
		ClaudeTextModel:     *claudeTextModel,
		ApprovalTimeout:     *approvalTimeout,
		DotfilesAgentsDir:   *dotfilesAgentsDir,

		TokenSaverReleaseBase: *tokenSaverReleaseBase,
		ProfilerOff:           !*profiler,
		SlowRequestThreshold:  *slowRequestThreshold,
	}, nil
}

// DataDirEnv names the data dir, as -data.dir does.
const DataDirEnv = "HARNESS_DATA_DIR"

// defaultDataDir is the data dir under the home directory when neither the
// data dir nor the database is named.
const defaultDataDir = ".harness"

// dataLocation is where the harness keeps its data.
type dataLocation struct{ dir, source, db string }

// resolveDataDir settles the data dir and the database path. A named data
// dir wins; else a named database on disk keeps its folder as the data dir,
// as before there was one; else it is ~/.harness. An unnamed database is
// data.db in the data dir. An in-memory database with no named data dir has
// none: its data goes to temporary directories.
func resolveDataDir(fs *flag.FlagSet, getenv Getenv, dir, db string) (dataLocation, error) {
	home := func() (string, error) {
		if h := strings.TrimSpace(getenv("HOME")); h != "" {
			return h, nil
		}
		return os.UserHomeDir()
	}
	var loc dataLocation
	switch {
	case dir != "":
		loc.source = "the " + DataDirEnv + " environment variable"
		if isSet(fs, "data.dir") {
			loc.source = "the -data.dir flag"
		}
		if rest, ok := strings.CutPrefix(dir, "~/"); ok || dir == "~" {
			h, err := home()
			if err != nil {
				return dataLocation{}, fmt.Errorf("data dir %s: %w", dir, err)
			}
			dir = filepath.Join(h, rest)
		}
	case db == ":memory:":
		return dataLocation{db: db}, nil
	case db != "":
		loc.source = "the database path (DB_PATH)"
		if isSet(fs, "db") {
			loc.source = "the database path (-db)"
		}
		dir = filepath.Dir(db)
	default:
		h, err := home()
		if err != nil {
			return dataLocation{}, fmt.Errorf("the default data dir: %w", err)
		}
		loc.source = "the default (~/" + defaultDataDir + ")"
		dir = filepath.Join(h, defaultDataDir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dataLocation{}, err
	}
	loc.dir, loc.db = abs, db
	if loc.db == "" {
		loc.db = filepath.Join(abs, "data.db")
	}
	return loc, nil
}

// isSet reports whether the flag name was given on the command line.
func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

func envOr(getenv Getenv, key, fallback string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envBool(getenv Getenv, key string, fallback bool) bool {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func envDuration(getenv Getenv, key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}

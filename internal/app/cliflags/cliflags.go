// Package cliflags resolves the process configuration shared by every binary
// that boots the application graph (the server and the TUI): the same flags,
// the same environment fallbacks, the same defaults. Each main adds its own
// extra flags to the FlagSet before calling Parse.
package cliflags

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

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
	dbPath := fs.String("db", envOr(getenv, "DB_PATH", "data.db"), "SQLite database path (e.g. data.db or :memory:)")
	devMode := fs.Bool("dev", envBool(getenv, "DEV_MODE", false), "proxy ui://designer to the Vite dev server (run 'make web-dev' separately)")
	claudeBin := fs.String("claude.bin", envOr(getenv, "CLAUDE_BIN", "claude"), "path to the claude CLI binary")
	claudeLogStdout := fs.Bool("claude.log-stdout", envBool(getenv, "CLAUDE_LOG_STDOUT", true), "tee each session's raw stdout to the terminal")
	claudeTextModel := fs.String("claude.text-model", envOr(getenv, "CLAUDE_TEXT_MODEL", ""), "model for the one-shot text utilities (summarize/generate/translate); empty uses the CLI default")
	approvalTimeout := fs.Duration("approval.timeout", envDuration(getenv, "APPROVAL_TIMEOUT", 24*time.Hour), "how long a session waits for the user to answer an approval or question")

	if err := fs.Parse(args); err != nil {
		return app.Config{}, err
	}

	root := envOr(getenv, "PROJECT_ROOT", "")
	if root == "" {
		root, _ = os.Getwd()
	}

	return app.Config{
		HTTPAddr:        *httpAddr,
		MCPAddr:         *mcpAddr,
		DBPath:          *dbPath,
		DevMode:         *devMode,
		Root:            root,
		ClaudeBin:       *claudeBin,
		ClaudeLogStdout: *claudeLogStdout,
		ClaudeTextModel: *claudeTextModel,
		ApprovalTimeout: *approvalTimeout,
	}, nil
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

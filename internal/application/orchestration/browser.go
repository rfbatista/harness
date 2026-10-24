package orchestration

import (
	"os"
	"path/filepath"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/ports"
)

// browserServerName names the browser MCP server in a session's config, and
// so its tools: mcp__browser__* (ADR 0025).
const browserServerName = "browser"

// playwrightMCPVersion is the @playwright/mcp release npx starts. It is
// pinned: raising it is a code change.
const playwrightMCPVersion = "0.0.83"

// withBrowserServer gives a sandboxed session the browser MCP server, with its
// tools allowed. It runs as a child of claude, so Chrome runs inside the agent
// sandbox (ADR 0024); --isolated keeps the profile in memory.
//
// Under seatbelt, tmpDir is the session's temp dir and the server runs with
// what Playwright needs there. Its unix socket lives under PWTEST_SOCKETS_DIR,
// and a path over 103 bytes is refused, which the default base, the system's
// temp dir, always is. Its server registry lives under the home it sees (on
// macOS os.homedir(), ~/Library/Caches), which the sandbox closes: HOME is the
// temp dir for this server alone, not for claude, and npm, which npx runs
// first, keeps the cache it always had. Without a tmpDir the server starts as
// it is.
func withBrowserServer(cfg *llmkit.SessionConfig, tmpDir, userHome string) {
	spec := llmkit.MCPServerSpec{
		Name:      browserServerName,
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@playwright/mcp@" + playwrightMCPVersion, "--headless", "--isolated", "--browser", "chrome", "--no-sandbox"},
	}
	if tmpDir != "" {
		spec.Env = map[string]string{ports.PlaywrightSocketsEnv: tmpDir, "HOME": tmpDir}
		if cache := npmCache(userHome); cache != "" {
			spec.Env["npm_config_cache"] = cache
		}
	}
	cfg.MCPServers = append(cfg.MCPServers, spec)
	// A bare server name allows every tool the server offers.
	cfg.AllowedTools = append(cfg.AllowedTools, "mcp__"+browserServerName)
}

// npmCache is the npm cache the toolchain allowlist opens: npm_config_cache
// when the server's env sets it, else ~/.npm.
func npmCache(userHome string) string {
	if c := os.Getenv("npm_config_cache"); c != "" {
		return c
	}
	if userHome == "" {
		return ""
	}
	return filepath.Join(userHome, ".npm")
}

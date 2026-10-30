package app

import (
	"path/filepath"

	"operators-mcp/internal/ports"
)

// allowSources is what the standing sandbox allowlist is built from.
type allowSources struct {
	// Home is the person's home directory.
	Home string
	// ToolsDir is where the harness installs token savers.
	ToolsDir string
	// ClaudeDirs are the directories the agent's claude runs from.
	ClaudeDirs []string
	// Getenv reads the server's environment, where a toolchain is moved
	// from its default place.
	Getenv func(string) string
}

// sandboxAllowlist is what every agent session may touch beside its own
// worktree and the paths the person added: the token savers' install dir and
// claude's (read), and the toolchains and package caches builds and tests
// use (read-write for the caches, read for the toolchains), at their
// well-known places under the home or where the server's environment moves
// them. It never names the person's Claude state: legacy transcripts are read
// by the server alone.
func sandboxAllowlist(src allowSources) []ports.SandboxPath {
	var out []ports.SandboxPath
	add := func(path string, write bool) {
		if path != "" {
			out = append(out, ports.SandboxPath{Path: filepath.Clean(path), Write: write})
		}
	}
	add(src.ToolsDir, false)
	for _, d := range src.ClaudeDirs {
		add(d, false)
	}
	h := func(parts ...string) string {
		if src.Home == "" {
			return ""
		}
		return filepath.Join(append([]string{src.Home}, parts...)...)
	}
	// Caches and package stores the toolchains write.
	for _, p := range [][]string{
		{"go"}, {"Library", "Caches", "go-build"}, {".cache", "go-build"},
		{".npm"}, {".cargo"}, {".cache", "pip"}, {"Library", "Caches", "pip"},
		{"Library", "pnpm"}, {".local", "share", "pnpm"}, {".cache", "pnpm"},
		{".yarn"}, {".cache", "yarn"}, {"Library", "Caches", "Yarn"},
		{"Library", "Caches", "Homebrew"}, {".cache", "Homebrew"},
		// rtk keeps its config and history where it always did.
		{".config", "rtk"}, {".local", "share", "rtk"}, {"Library", "Application Support", "rtk"},
		// uv's cache, tools and managed Pythons: stdio MCP servers run by
		// uv or uvx need them.
		{".cache", "uv"}, {".local", "share", "uv"},
	} {
		add(h(p...), true)
	}
	// Toolchains and version managers, read only.
	for _, p := range [][]string{
		{".rustup"}, {".nvm"}, {".asdf"}, {".local", "share", "mise"}, {"sdk"},
		{".pyenv"}, {".local", "bin"}, {".volta"}, {".bun"},
		// podman's config and its data, which holds the machine's SSH
		// identity: stdio MCP servers run by podman need them.
		{".config", "containers"}, {".local", "share", "containers"},
	} {
		add(h(p...), false)
	}
	if src.Getenv != nil {
		for _, k := range []string{"GOPATH", "GOCACHE", "GOMODCACHE", "CARGO_HOME", "npm_config_cache", "PNPM_HOME", "YARN_CACHE_FOLDER", "PIP_CACHE_DIR", "UV_CACHE_DIR", "UV_TOOL_DIR", "UV_PYTHON_INSTALL_DIR"} {
			add(src.Getenv(k), true)
		}
		for _, k := range []string{"RUSTUP_HOME", "NVM_DIR", "GOROOT", "CONTAINERS_CONF"} {
			add(src.Getenv(k), false)
		}
		under := func(base, sub string, write bool) {
			if b := src.Getenv(base); b != "" {
				add(filepath.Join(b, sub), write)
			}
		}
		under("XDG_CACHE_HOME", "uv", true)
		under("XDG_DATA_HOME", "uv", true)
		under("XDG_CONFIG_HOME", "containers", false)
		under("XDG_DATA_HOME", "containers", false)
		// The podman machine's API socket sits under the person's own
		// temporary directory on macOS (the server's, not the session's) and
		// under the runtime dir on Linux. A client connects to it, so it is
		// writable.
		under("TMPDIR", "podman", true)
		under("XDG_RUNTIME_DIR", "podman", true)
	}
	return out
}

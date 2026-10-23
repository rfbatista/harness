// Package runtime is the fx module of the runtime stack interactive agents
// run on: the Agent, Shell and PTY adapters, and the terminal host that
// composes them. tui-client includes it to run agents itself; the server
// includes it to host them.
package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/agents/claudecli"
	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/adapter/out/agents/command"
	"operators-mcp/internal/adapter/out/ptyunix"
	"operators-mcp/internal/adapter/out/shell"
	"operators-mcp/internal/adapter/out/termhost"
	"operators-mcp/internal/ports"
)

// Config picks the adapters.
type Config struct {
	// ClaudeBin is the claude binary; empty lets the shell find "claude".
	ClaudeBin string
	// Shell is "direct" (the default: run the program itself) or "login" (run
	// it through the user's login shell, so their profile sets PATH).
	Shell string
	// ProxyURL is the LLM proxy claude calls through (ADR 0006); empty
	// leaves claude calling the API directly, as tui-client does for now.
	ProxyURL string
	// ConfigDir is the Claude home every claude runs in (ADR 0018); empty
	// runs the person's own. With one, claude starts isolated from the
	// person's configuration and trusts each session's worktree in it.
	ConfigDir string
	// Sandbox confines the sessions whose spec carries a policy; nil refuses
	// them (a policy never runs unconfined by accident).
	Sandbox ports.SandboxDriver
	// SandboxAllow is added to every policy on this host, beside the Claude
	// home and claude's install dir, which are always allowed.
	SandboxAllow []ports.SandboxPath
}

// Module provides ports.Launcher (claude, through llmkit), ports.Shell,
// ports.PTY and ports.TerminalHost.
// Stopping it kills every agent still running on the host.
var Module = fx.Module("runtime",
	fx.Provide(
		newShell,
		fx.Annotate(ptyunix.New, fx.As(new(ports.PTY))),
		fx.Annotate(newClaude, fx.As(new(ports.Launcher))),
		fx.Annotate(newHost, fx.As(new(ports.TerminalHost))),
	),
)

func newShell(cfg Config) (ports.Shell, error) {
	switch cfg.Shell {
	case "", "direct":
		return shell.Direct{}, nil
	case "login":
		return shell.Login{}, nil
	}
	return nil, fmt.Errorf("unknown shell %q: want direct or login", cfg.Shell)
}

// newClaude starts interactive claude through llmkit (ADR 0007). An empty
// ClaudeBin lets the shell find "claude".
func newClaude(cfg Config) claudecli.Launcher {
	bin := cfg.ClaudeBin
	if bin == "" {
		bin = "claude"
	}
	l := claudecli.NewLauncher(claude.New(llmkit.Options{
		Bin: bin, ProxyURL: cfg.ProxyURL, ConfigDir: cfg.ConfigDir, Isolated: cfg.ConfigDir != "",
	}))
	allow := slices.Clone(cfg.SandboxAllow)
	if cfg.ConfigDir != "" {
		l = l.WithHome(claudehome.NewHome(cfg.ConfigDir))
		allow = append(allow, ports.SandboxPath{Path: cfg.ConfigDir, Write: true})
	}
	for _, d := range ClaudeInstallDirs(bin) {
		allow = append(allow, ports.SandboxPath{Path: d})
	}
	if cfg.Sandbox != nil {
		l = l.WithSandbox(cfg.Sandbox, allow...)
	}
	return l
}

// ClaudeInstallDirs are the directories a sandboxed claude must read to run:
// where bin is, and where its symlinks lead. A bare name the shell finds
// has none.
func ClaudeInstallDirs(bin string) []string {
	if !filepath.IsAbs(bin) {
		return nil
	}
	dirs := []string{filepath.Dir(bin)}
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		dirs = append(dirs, filepath.Dir(r))
		// <managed dir>/versions/<version>/claude: the whole managed dir.
		if filepath.Base(filepath.Dir(filepath.Dir(r))) == "versions" {
			dirs = append(dirs, filepath.Dir(filepath.Dir(filepath.Dir(r))))
		}
	}
	return slices.Compact(dirs)
}

// newHost runs claude for sessions, and plain command lines for the
// application runs started from them (the "command" kind).
func newHost(lc fx.Lifecycle, sh ports.Shell, pty ports.PTY, claudeLauncher ports.Launcher) *termhost.Host {
	h := termhost.New(sh, pty, command.New()).WithLaunchers(claudeLauncher)
	lc.Append(fx.StopHook(func(ctx context.Context) error { return h.Shutdown(ctx) }))
	return h
}

// Package runtime is the fx module of the runtime stack interactive agents
// run on: the Agent, Shell and PTY adapters, and the terminal host that
// composes them. tui-client includes it to run agents itself; the server
// includes it to host them.
package runtime

import (
	"context"
	"fmt"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/agents/claudecli"
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
	return claudecli.NewLauncher(claude.New(llmkit.Options{Bin: bin, ProxyURL: cfg.ProxyURL}))
}

// newHost runs claude for sessions, and plain command lines for the
// application runs started from them (the "command" kind).
func newHost(lc fx.Lifecycle, sh ports.Shell, pty ports.PTY, claudeLauncher ports.Launcher) *termhost.Host {
	h := termhost.New(sh, pty, command.New()).WithLaunchers(claudeLauncher)
	lc.Append(fx.StopHook(func(ctx context.Context) error { return h.Shutdown(ctx) }))
	return h
}

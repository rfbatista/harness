// Command tui-client is coding_pool in the terminal: pick a project, then one
// of its tasks, then open agent sessions in it. Each session is interactive
// claude in its own git worktree, running in a pane.
//
//	tui-client [--api http://localhost:8080] [--token TOKEN] [--run server|tui] [--shell direct|login]
//
// The coding_pool server must be running (`make air`); it provisions and
// records the sessions. With --run server (the default) it also runs them:
// they outlive this client, and the next one reattaches. With --run tui this
// process runs them, and they end when it does; the claude binary then comes
// from $CLAUDE_BIN, else PATH — the process's with --shell direct, the login
// shell's with --shell login.
// fx's own log goes to ~/.coding-pool/tui-client.log, never to the terminal
// the UI draws on.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/tuiclient/wire"
)

func main() {
	os.Exit(run())
}

func run() int {
	api := flag.String("api", envOr("CODING_POOL_API", "http://localhost:8080"), "coding_pool server URL")
	token := flag.String("token", os.Getenv("CODING_POOL_TOKEN"), "coding_pool server bearer token")
	run := flag.String("run", envOr("CODING_POOL_RUN", "server"), "where sessions run: server (they outlive this client) or tui (in this process)")
	sh := flag.String("shell", envOr("CODING_POOL_SHELL", "direct"), "with --run tui, how sessions start: direct, or login to run them through your login shell")
	flag.Parse()

	runsOn := domain.Runner(*run)
	if !runsOn.Valid() {
		fmt.Fprintf(os.Stderr, "tui-client: --run %q: want server or tui\n", *run)
		return 2
	}
	host, _ := os.Hostname()
	bin := os.Getenv("CLAUDE_BIN")
	if runsOn == domain.RunnerTUI && bin == "" && *sh == "direct" {
		found, err := exec.LookPath("claude")
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui-client: claude not found on PATH (set CLAUDE_BIN, or use --shell login):", err)
			return 1
		}
		bin = found
	}
	logw, closeLog := logFile()
	defer closeLog()

	app := fx.New(
		wire.Options(wire.Config{API: *api, Token: *token, Claude: bin, Shell: *sh, RunsOn: runsOn, RunnerHost: host}),
		fx.WithLogger(func() fxevent.Logger { return &fxevent.ConsoleLogger{W: logw} }),
	)
	startCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		fmt.Fprintln(os.Stderr, "tui-client:", err)
		return 1
	}
	sig := <-app.Wait()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelStop()
	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintln(os.Stderr, "tui-client:", err)
		return 1
	}
	return sig.ExitCode
}

// logFile is where fx logs; io.Discard when it cannot be opened.
func logFile() (io.Writer, func()) {
	home, err := os.UserHomeDir()
	if err != nil {
		return io.Discard, func() {}
	}
	dir := filepath.Join(home, ".coding-pool")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return io.Discard, func() {}
	}
	f, err := os.OpenFile(filepath.Join(dir, "tui-client.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return io.Discard, func() {}
	}
	return f, func() { _ = f.Close() }
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

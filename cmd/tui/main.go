// Command tui runs coding_pool as a terminal application: the full server
// (HTTP API, MCP endpoint, embedded web UI and the claude CLI's loopback
// mounts) with a Bubble Tea front-end on top. Quitting the TUI stops the
// server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"operators-mcp/internal/app"
	"operators-mcp/internal/app/cliflags"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.CommandLine
	logPath := fs.String("log", defaultLogPath(), "file slog writes to while the TUI owns the terminal")
	themeMode := fs.String("theme", "auto", "colour theme: auto, dark or light")
	cfg, err := cliflags.Parse(fs, os.Args[1:], os.Getenv)
	if err != nil {
		return err
	}
	// The terminal belongs to the UI: nothing else may write to stdout.
	cfg.ClaudeLogStdout = false
	cfg.TUI = app.TUIConfig{LogPath: *logPath, ThemeMode: *themeMode}

	logFile, err := openLog(*logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))

	fxApp, program := app.NewTUI(cfg)
	if err := fxApp.Err(); err != nil {
		return err
	}
	startCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := fxApp.Start(startCtx); err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	slog.Info("tui started", "http", cfg.HTTPAddr, "mcp", cfg.MCPAddr, "db", cfg.DBPath)

	_, runErr := program.Run()

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStop()
	if err := fxApp.Stop(stopCtx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	return runErr
}

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "tui.log"
	}
	return filepath.Join(home, ".coding-pool", "tui.log")
}

func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return f, nil
}

package app

import (
	"context"
	"log/slog"
	"runtime"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/psprocs"
	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// MemoryWatchModule runs the vigia de memória (ADR 0005) over the real process
// table. It is macOS only: elsewhere sessions are not watched. Until the
// events and notification tickets, what it sees goes to the log.
var MemoryWatchModule = fx.Module("memorywatch", fx.Invoke(useMemoryWatch))

func useMemoryWatch(lc fx.Lifecycle, orch *orchestration.Service) {
	if runtime.GOOS != "darwin" {
		return
	}
	w := memorywatch.New(memorywatch.Deps{Table: psprocs.Table{}, Signal: psprocs.Signaler{}, Sink: logSink{}})
	orch.MemoryWatcher = w
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { w.Start(); return nil },
		OnStop:  func(context.Context) error { w.Close(); return nil },
	})
}

// logSink records encerramentos por memória in the log, with the process name
// and its first argument only, never a full command line.
type logSink struct{}

func (logSink) Sampled(domain.MemorySample) {}

func (logSink) Killed(k domain.MemoryKill) {
	slog.Warn("session ended a process for memory", "session", k.SessionID, "pid", k.PID, "command", k.Command,
		"rss", k.RSSBytes, "tree_rss", k.TreeRSS, "budget", k.Budget, "signal", k.Signal)
}

package app

import (
	"context"
	"runtime"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/psprocs"
	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/application/orchestration"
)

// MemoryWatchModule runs the vigia de memória (ADR 0005) over the real process
// table. It is macOS only: elsewhere sessions are not watched. What it sees goes to the orchestration, which tells the agent and the inbox.
var MemoryWatchModule = fx.Module("memorywatch", fx.Invoke(useMemoryWatch))

func useMemoryWatch(lc fx.Lifecycle, orch *orchestration.Service) {
	if runtime.GOOS != "darwin" {
		return
	}
	w := memorywatch.New(memorywatch.Deps{Table: psprocs.Table{}, Signal: psprocs.Signaler{}, Sink: orch})
	orch.MemoryWatcher = w
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error { w.Start(); return nil },
		OnStop:  func(context.Context) error { w.Close(); return nil },
	})
}

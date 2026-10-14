package app

import (
	"log/slog"

	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/ports"
)

// UsageModule wires token usage (ADR 0006): the usage records' table and the
// service that is the LLM proxy's usage sink and the usage API's reader.
var UsageModule = fx.Module("usage",
	fx.Provide(newUsageService),
)

func newUsageService(db *gorm.DB, sessions ports.SessionRepository, tickets ports.TicketRepository) *usage.Service {
	return usage.New(sqlite.NewUsageRepository(db), usage.Readers{Sessions: sessions, Tickets: tickets},
		usage.Options{Logger: slog.Default()})
}

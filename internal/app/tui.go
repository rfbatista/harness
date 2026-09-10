package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/tui"
	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// TUIConfig is what the terminal front-end needs beyond the server Config.
type TUIConfig struct {
	// LogPath is where slog writes while the TUI owns the terminal, and what
	// the `d` key shows.
	LogPath string
	// ThemeMode is auto, dark or light.
	ThemeMode string
}

// TUIBackendModule binds the TUI's Backend port onto the application services.
// It is separate from TUIModule so tests can resolve the backend without
// constructing a program.
var TUIBackendModule = fx.Module("tui-backend",
	fx.Provide(fx.Annotate(backend.New, fx.As(new(backend.Backend)))),
)

// TUIModule provides the Bubble Tea program on top of the backend.
var TUIModule = fx.Module("tui",
	TUIBackendModule,
	fx.Provide(newTUIProgram),
)

func newTUIProgram(cfg Config, be backend.Backend) (*tea.Program, error) {
	mode, err := theme.ParseMode(cfg.TUI.ThemeMode)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	return tui.NewProgram(tui.Options{
		Backend:   be,
		Theme:     theme.For(mode),
		AutoTheme: mode == theme.ModeAuto,
		LogPath:   cfg.TUI.LogPath,
	}), nil
}

// tuiModules is the server graph plus the TUI: the TUI binary is the whole
// server with a terminal front-end, so the claude CLI's loopback endpoints and
// the web UI keep working underneath it.
func tuiModules(cfg Config) []fx.Option {
	return []fx.Option{
		fx.Supply(cfg),
		PersistenceModule,
		BlueprintModule,
		PlanningModule,
		WorkspacesModule,
		ExecutionModule,
		AgentRuntimeModule,
		TextProcessingModule,
		ToolingModule,
		ServerModule,
		TUIModule,
	}
}

// NewTUI builds the application with the TUI attached and returns the program
// to run on the main goroutine once the app has started. The fx app is built
// with NopLogger because stdout belongs to the terminal UI.
func NewTUI(cfg Config) (*fx.App, *tea.Program) {
	var program *tea.Program
	opts := append(tuiModules(cfg),
		fx.StopTimeout(stopTimeout),
		fx.Populate(&program),
		fx.NopLogger,
	)
	return fx.New(opts...), program
}

// ValidateTUI type-checks the TUI graph without opening anything.
func ValidateTUI(cfg Config) error {
	return fx.ValidateApp(tuiModules(cfg)...)
}

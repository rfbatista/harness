package app

import (
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/application/blueprint"
)

func tuiTestConfig(t *testing.T) Config {
	return Config{
		HTTPAddr: ":0",
		MCPAddr:  ":0",
		DBPath:   ":memory:",
		Root:     t.TempDir(),
		TUI:      TUIConfig{LogPath: t.TempDir() + "/tui.log", ThemeMode: "dark"},
	}
}

// TestTUIGraphValidates type-checks the server graph plus the TUI module.
func TestTUIGraphValidates(t *testing.T) {
	if err := ValidateTUI(tuiTestConfig(t)); err != nil {
		t.Fatalf("tui graph did not validate: %v", err)
	}
}

// TestTUIBackendReadsThroughServices builds the real backend from the graph
// and checks a project created through blueprint is visible to the TUI.
func TestTUIBackendReadsThroughServices(t *testing.T) {
	var (
		be backend.Backend
		bp *blueprint.Service
	)
	app := fx.New(
		fx.Supply(tuiTestConfig(t)),
		PersistenceModule,
		BlueprintModule,
		PlanningModule,
		WorkspacesModule,
		ExecutionModule,
		AgentRuntimeModule,
		TextProcessingModule,
		ToolingModule,
		TUIBackendModule,
		fx.Populate(&be, &bp),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("graph: %v", err)
	}
	if _, err := bp.CreateProject("demo", t.TempDir()); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if got := be.ListProjects(); len(got) != 1 || got[0].Name != "demo" {
		t.Fatalf("backend should see the project: %+v", got)
	}
	if got := be.ListTickets(""); len(got) != 0 {
		t.Fatalf("no tickets expected, got %d", len(got))
	}
}

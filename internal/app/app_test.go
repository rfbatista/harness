package app

import (
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/application/ports"
)

// TestGraphValidates ensures the full dependency graph is satisfiable: every
// provider's dependencies are met and there are no cycles. It type-checks the
// wiring without opening the database or starting any server.
func TestGraphValidates(t *testing.T) {
	cfg := Config{
		HTTPAddr: ":0",
		MCPAddr:  ":0",
		DBPath:   ":memory:",
		Root:     t.TempDir(),
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("fx graph did not validate: %v", err)
	}
}

// TestTextProcessingPortsResolve constructs the three text-processing ports.
// Nothing in the app consumes them yet, so TestGraphValidates would not notice
// a binding that names the wrong interface; asking fx to build them does.
func TestTextProcessingPortsResolve(t *testing.T) {
	var (
		sum ports.Summarizer
		gen ports.TextGenerator
		tr  ports.Translator
	)
	app := fx.New(
		fx.Supply(Config{ClaudeBin: "claude"}),
		TextProcessingModule,
		fx.Populate(&sum, &gen, &tr),
		fx.NopLogger,
	)
	if err := app.Err(); err != nil {
		t.Fatalf("text processing module: %v", err)
	}
	if sum == nil || gen == nil || tr == nil {
		t.Fatalf("ports unbound: summarizer=%v generator=%v translator=%v", sum, gen, tr)
	}
}

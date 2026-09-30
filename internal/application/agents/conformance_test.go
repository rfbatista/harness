package agents

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestAgentCatalogConformance(t *testing.T) {
	portstest.AgentCatalogConformance(t, func(t *testing.T) ports.AgentCatalog {
		db, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		return NewService(sqlite.NewAgentRepository(db), sqlite.NewPromptRepository(db), nil, nil)
	})
}

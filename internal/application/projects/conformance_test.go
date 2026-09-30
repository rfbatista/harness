package projects

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func TestProjectsConformance(t *testing.T) {
	portstest.ProjectsConformance(t, func(t *testing.T) ports.Projects {
		db, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		return NewService(sqlite.NewProjectRepository(db), sqlite.NewRepositoryRepository(db), nil)
	})
}

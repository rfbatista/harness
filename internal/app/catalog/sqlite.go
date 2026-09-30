package catalog

import (
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
)

// SQLiteDeps returns Deps with every repository backed by db. PathMatcher,
// TreeLister, Publisher and DefaultRoot are left for the caller.
func SQLiteDeps(db *gorm.DB) Deps {
	return Deps{
		Projects:        sqlite.NewProjectRepository(db),
		Repositories:    sqlite.NewRepositoryRepository(db),
		Zones:           sqlite.NewZoneRepository(db),
		BoundedContexts: sqlite.NewBoundedContextRepository(db),
		Agents:          sqlite.NewAgentRepository(db),
		Prompts:         sqlite.NewPromptRepository(db),
		Skills:          sqlite.NewSkillRepository(db),
		MCPServers:      sqlite.NewMCPServerRepository(db),
		Tools:           sqlite.NewToolRepository(db),
		Settings:        sqlite.NewSettingsRepository(db),
	}
}

package app

import (
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/application/contextmap"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// newContextMap wires the context map: generated from the default branch,
// gated by the effective configuration, announced on the project feed.
func newContextMap(store ports.ContextMapStore, cat catalog.Catalog, git ports.ArchitectureGit, settings *agentsystem.Service, orch *orchestration.Service) *contextmap.Service {
	return contextmap.New(contextmap.Deps{
		Store:        store,
		Repositories: cat.Projects,
		Git:          git,
		Settings:     settings,
		Architecture: cat.RepositoryArchitectures,
		Feed:         orch,
	})
}

// bindContextMap gives the orchestration service the maps it delivers at start.
func bindContextMap(orch *orchestration.Service, cm *contextmap.Service) { orch.ContextMaps = cm }

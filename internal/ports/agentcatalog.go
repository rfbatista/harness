package ports

import "operators-mcp/internal/domain"

// CatalogReader reads the agent catalog: the folder of agents, bundles,
// skills and MCP servers in the harness data directory (ADR 0030).
type CatalogReader interface {
	// ReadCatalog reads the whole catalog. A catalog file that does not parse
	// is reported in CatalogView.Broken, with the lists of the last version
	// that did.
	ReadCatalog() (*domain.CatalogView, error)
	// ReadCatalogSkill reads one skill's full content. SKILL_NOT_FOUND when
	// there is none.
	ReadCatalogSkill(name string) (*domain.CatalogSkillView, error)
	// CheckEnvVars says whether each variable is set in the harness server's
	// environment. Never a value.
	CheckEnvVars(names []string) map[string]bool
}

package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

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

// CatalogEditor reads and writes the agent catalog. Every write is guarded by
// file versions, can be previewed, and the latest one can be undone.
type CatalogEditor interface {
	CatalogReader
	SaveCatalogAgent(in domain.SaveCatalogAgent) (*domain.CatalogWriteResult, error)
	DeleteCatalogAgent(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error)
	SaveCatalogBundle(in domain.SaveCatalogBundle) (*domain.CatalogWriteResult, error)
	DeleteCatalogBundle(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error)
	SaveCatalogSkill(in domain.SaveCatalogSkill) (*domain.CatalogWriteResult, error)
	DeleteCatalogSkill(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error)
	SaveCatalogMCPServer(in domain.SaveCatalogMCPServer) (*domain.CatalogWriteResult, error)
	DeleteCatalogMCPServer(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error)
	// TestCatalogMCPServer starts or calls the server once, outside any agent
	// sandbox, and says what it found. It stores nothing.
	TestCatalogMCPServer(ctx context.Context, in domain.TestCatalogMCPServer) (*domain.CatalogProbeResult, error)
	GetProjectMCPServers(ctx context.Context, projectID string) (*domain.CatalogProjectMCPServers, error)
	SetProjectMCPServers(ctx context.Context, in domain.SetCatalogProjectMCPServers) (*domain.CatalogProjectMCPServers, error)
	UndoCatalogChange(in domain.UndoCatalogChange) (*domain.CatalogWriteResult, error)
}

// CatalogChangeFeed follows the catalog's writes.
type CatalogChangeFeed interface {
	// FollowCatalogChanges delivers each write's changed paths until ctx
	// ends. The channel closes when ctx ends or the follower fell behind.
	FollowCatalogChanges(ctx context.Context) (<-chan domain.CatalogChanged, error)
}

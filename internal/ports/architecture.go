package ports

import "operators-mcp/internal/domain"

// Driving ports of the architecture context: how a project's code is carved
// into zones and bounded contexts, and its file tree. *architecture.Service
// satisfies them.

// ZoneCatalog manages a project's zones and the paths assigned to them.
type ZoneCatalog interface {
	ListZones(projectID string) []*domain.Zone
	GetZone(zoneID string) *domain.Zone
	CreateZone(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	UpdateZone(zoneID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error)
	AssignPathToZone(zoneID, path string) (*domain.Zone, error)
	UnassignPathFromZone(zoneID, path string) (*domain.Zone, error)
}

// BoundedContextCatalog manages a project's bounded contexts and which zones
// belong to them.
type BoundedContextCatalog interface {
	ListBoundedContexts(projectID string) []*domain.BoundedContext
	GetBoundedContext(id string) *domain.BoundedContext
	CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error)
	DeleteBoundedContext(id string) error
	AssignZoneToBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error)
	UnassignZoneFromBoundedContext(zoneID string) (*domain.Zone, error)
}

// ArchitectureMap is what a session reads about how its project is carved
// up: the bounded contexts and the zones that belong to them.
type ArchitectureMap interface {
	ListBoundedContexts(projectID string) []*domain.BoundedContext
	ListZones(projectID string) []*domain.Zone
}

// PathExplorer reads a project's file tree. root overrides the project's root
// directory when set.
type PathExplorer interface {
	ListMatchingPaths(root, projectID, pattern string) ([]string, error)
	ListTree(root, projectID string) (*domain.TreeNode, error)
}

// ZoneReader is what other contexts read about zones.
type ZoneReader interface {
	GetZone(zoneID string) *domain.Zone
}

// Architecture is the whole architecture context.
type Architecture interface {
	ZoneCatalog
	BoundedContextCatalog
	PathExplorer
}

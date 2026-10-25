// Package repoarch is the repository architecture context: one structured
// record per repository (ADR 0032) — its layers, who may change them, and the
// rules that guard them — saved only by the person, read by sessions, hooks
// and the web UI.
//
// The record is the source of truth. A save stores a new version and turns the
// layers into repository zones in the same transaction; the architecture
// file in the repository is an export (ADR 0034).
package repoarch

import (
	"context"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RepositoryArchitectures = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Store        ports.ArchitectureRecordStore
	Repositories ports.RepositoryReader
	Agents       ports.AgentLister
	// Contexts lists a project's bounded contexts; nil knows none.
	Contexts interface {
		ListBoundedContexts(projectID string) []*domain.BoundedContext
	}
	// Git reads the checkout's default branch; nil leaves previews and
	// discovery unavailable.
	Git ports.ArchitectureGit
	// MCPServers is the harness MCP catalog; nil offers no tool from it.
	MCPServers interface{ ListMCPServers() []*domain.MCPServer }
}

// Service implements the repository architecture use cases.
type Service struct {
	store    ports.ArchitectureRecordStore
	repos    ports.RepositoryReader
	agents   ports.AgentLister
	contexts interface {
		ListBoundedContexts(projectID string) []*domain.BoundedContext
	}
	feed ports.ProjectChangeSink // nil until the orchestration is wired
	git  ports.ArchitectureGit
	mcp  interface{ ListMCPServers() []*domain.MCPServer }

	sandboxDriver   func() ports.SandboxDriver
	sandboxSettings SandboxSource

	mu        sync.Mutex
	checkouts map[string]*sync.Mutex
}

// SandboxSource is the person's sandbox configuration, as far as discovery
// reads it.
type SandboxSource interface {
	Enabled() bool
	Paths() []ports.SandboxPath
}

// UseSandbox tells discovery which sandbox the platform has (the driver may
// be nil) and what the person added to it.
func (s *Service) UseSandbox(driver func() ports.SandboxDriver, settings SandboxSource) {
	s.sandboxDriver, s.sandboxSettings = driver, settings
}

// NewService returns the repository architecture context.
func NewService(d Deps) *Service {
	return &Service{store: d.Store, repos: d.Repositories, agents: d.Agents, contexts: d.Contexts, git: d.Git, mcp: d.MCPServers, checkouts: map[string]*sync.Mutex{}}
}

// UseFeed sets where saves, deletes and exports are announced: the project
// feed, which the orchestration owns.
func (s *Service) UseFeed(feed ports.ProjectChangeSink) { s.feed = feed }

// Subscribe registers the context's reactions to other contexts' events: a
// repository's record goes with it, so no orphan rules keep firing.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.RepositoryDeleted) error {
		if err := s.store.Delete(ctx, ev.RepositoryID); err != nil {
			return err
		}
		s.announce(ev.ProjectID, ev.RepositoryID, 0)
		return nil
	})
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		return s.store.DeleteByProject(ctx, ev.ProjectID)
	})
}

func (s *Service) announce(projectID, repositoryID string, version int) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{ArchitectureChanged: &ports.ArchitectureChange{
		RepositoryID: repositoryID, Version: version, ProjectID: projectID,
	}})
}

// GetRepositoryArchitecture implements ports.RepositoryArchitectures. It
// never validates: a record whose agent was renamed since the save still
// reads, and is refused on the next save.
func (s *Service) GetRepositoryArchitecture(ctx context.Context, repositoryID string) (*domain.RepositoryArchitecture, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.Get(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		known, err := s.known(ctx, repo.ProjectID)
		if err != nil {
			return nil, err
		}
		return &domain.RepositoryArchitecture{
			RepositoryID: repositoryID, ProjectID: repo.ProjectID,
			Record: domain.StarterArchitecture(repo.DisplayName(), known.Agents),
			Export: domain.ArchitectureExport{State: domain.ExportNeverExported, Path: domain.ArchitectureFilePath},
		}, nil
	}
	return s.architectureOf(ctx, repo, stored), nil
}

func (s *Service) architectureOf(ctx context.Context, repo *domain.Repository, st *ports.StoredArchitecture) *domain.RepositoryArchitecture {
	return &domain.RepositoryArchitecture{
		RepositoryID: st.RepositoryID, ProjectID: st.ProjectID, Exists: true, Version: st.Version, UpdatedAt: st.UpdatedAt,
		Record: st.Record, Export: s.exportOf(ctx, repo, st),
	}
}

// exportOf is the export block of a stored record.
func (s *Service) exportOf(ctx context.Context, repo *domain.Repository, st *ports.StoredArchitecture) domain.ArchitectureExport {
	return domain.ArchitectureExport{
		State: s.exportState(ctx, repo, st), Path: domain.ArchitectureFilePath,
		ExportedVersion: st.ExportedVersion, ExportedCommit: st.ExportedCommit, ExportedAt: st.ExportedAt,
	}
}

// lockCheckout serializes the writes to one checkout.
func (s *Service) lockCheckout(root string) func() {
	s.mu.Lock()
	l, ok := s.checkouts[root]
	if !ok {
		l = &sync.Mutex{}
		s.checkouts[root] = l
	}
	s.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// SaveRepositoryArchitecture implements ports.RepositoryArchitectures.
func (s *Service) SaveRepositoryArchitecture(ctx context.Context, repositoryID string, baseVersion int, record domain.ArchitectureRecord) (*domain.RepositoryArchitecture, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	known, err := s.known(ctx, repo.ProjectID)
	if err != nil {
		return nil, err
	}
	record.Normalize()
	if problems := record.Validate(known.ArchitectureKnown); len(problems) > 0 {
		return nil, invalidArchitecture(problems)
	}
	if record.Repository == "" {
		record.Repository = repo.DisplayName()
	}
	zones := record.LayerZones(repositoryID, repo.ProjectID)
	for i, l := range record.Layers {
		zones[i].BoundedContextID = known.contextIDs[l.BoundedContext]
	}
	stored, err := s.store.Save(ctx, repositoryID, repo.ProjectID, baseVersion, record, zones)
	if err != nil {
		return nil, err
	}
	s.announce(repo.ProjectID, repositoryID, stored.Version)
	return s.architectureOf(ctx, repo, stored), nil
}

// DeleteRepositoryArchitecture implements ports.RepositoryArchitectures.
func (s *Service) DeleteRepositoryArchitecture(ctx context.Context, repositoryID string) error {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, repositoryID); err != nil {
		return err
	}
	s.announce(repo.ProjectID, repositoryID, 0)
	return nil
}

// knownNames is what a record's names are checked against, and the ids of the
// bounded contexts by name.
type knownNames struct {
	domain.ArchitectureKnown
	contextIDs map[string]string
}

func (s *Service) known(ctx context.Context, projectID string) (knownNames, error) {
	k := knownNames{
		ArchitectureKnown: domain.ArchitectureKnown{Agents: map[string]bool{}, BoundedContexts: map[string]bool{}},
		contextIDs:        map[string]string{},
	}
	list, err := s.agents.ListAgents(ctx)
	if err != nil {
		return k, err
	}
	for _, a := range list {
		k.Agents[a.Name] = true
	}
	if s.contexts != nil {
		for _, bc := range s.contexts.ListBoundedContexts(projectID) {
			k.BoundedContexts[bc.Name] = true
			k.contextIDs[bc.Name] = bc.ID
		}
	}
	return k, nil
}

func invalidArchitecture(problems []domain.ArchFieldError) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "ARCHITECTURE_INVALID", Message: "the architecture record is invalid"},
		Details:         problems,
	}
}

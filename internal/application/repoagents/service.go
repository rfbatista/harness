// Package repoagents decides which agents a repository assigns (ADR 0061): the
// project's enabled agents minus the repository's list of unassigned agents.
package repoagents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Deps are what the service reads and writes.
type Deps struct {
	Lists        ports.RepositoryAgentListStore
	Projects     ports.ProjectRepository
	Repositories ports.RepositoryRepository
	// Catalog names the agents, in catalog order.
	Catalog ports.CatalogReader
	// Sessions counts an agent's running sessions in a repository.
	Sessions ports.SessionRepository
	// Agents maps a session's agent id to its catalog name; nil reads the id
	// as the name.
	Agents ports.AgentLister
}

// Service implements ports.RepositoryAgents.
type Service struct {
	d Deps

	// mu serialises writes, so a version is checked and replaced at once.
	mu   sync.Mutex
	feed *feed
}

// New returns the service.
func New(d Deps) *Service { return &Service{d: d, feed: newFeed()} }

var _ ports.RepositoryAgents = (*Service)(nil)

func notFound(code, msg string) error { return &domain.StructuredError{Code: code, Message: msg} }

// repository loads the repository and its project.
func (s *Service) repository(ctx context.Context, id string) (*domain.Repository, *domain.Project, error) {
	repo := s.d.Repositories.Get(ctx, id)
	if repo == nil {
		return nil, nil, notFound("REPOSITORY_NOT_FOUND", "repository "+id+" not found")
	}
	proj := s.d.Projects.Get(ctx, repo.ProjectID)
	if proj == nil {
		return nil, nil, notFound("PROJECT_NOT_FOUND", "project "+repo.ProjectID+" not found")
	}
	return repo, proj, nil
}

// GetRepositoryAgents reads a repository's Agents tab.
func (s *Service) GetRepositoryAgents(ctx context.Context, repositoryID string) (*domain.RepositoryAgentsView, error) {
	repo, proj, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, repo, proj)
}

func (s *Service) view(ctx context.Context, repo *domain.Repository, proj *domain.Project) (*domain.RepositoryAgentsView, error) {
	list, err := s.d.Lists.Get(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	cat, err := s.d.Catalog.ReadCatalog()
	if err != nil {
		return nil, err
	}
	running := s.runningByAgent(ctx, repo)
	mode := domain.AgentsAll
	if proj.EnabledAgentsMode == domain.AgentsSelected {
		mode = domain.AgentsSelected
	}
	v := &domain.RepositoryAgentsView{
		Repository:  domain.RepositoryAgentsRepo{ID: repo.ID, Name: repo.Name},
		ListVersion: list.Version,
		Agents:      []domain.RepositoryAgentRow{},
	}
	enabled := 0
	for _, a := range cat.Agents {
		isEnabled := proj.AgentEnabled(a.Name)
		if isEnabled {
			enabled++
		}
		if !isEnabled && !list.Has(a.Name) {
			continue
		}
		state := domain.AgentAssigned
		switch {
		case !isEnabled:
			state = domain.AgentNotEnabled
		case list.Has(a.Name):
			state = domain.AgentUnassigned
		}
		v.Agents = append(v.Agents, domain.RepositoryAgentRow{
			Name: a.Name, Description: a.Description, State: state, Unassigned: list.Has(a.Name),
			RunningSessions: running[a.Name],
		})
	}
	v.Project = domain.RepositoryAgentsProject{ID: proj.ID, Name: proj.Name, AgentsMode: mode, EnabledCount: enabled, CatalogCount: len(cat.Agents)}
	return v, nil
}

// runningStatuses are the statuses that count as running.
var runningStatuses = []domain.SessionStatus{
	domain.SessionStarting, domain.SessionRunning, domain.SessionIdle,
	domain.SessionThinking, domain.SessionWaitingApproval,
}

// runningByAgent counts the repository's running sessions by agent name.
func (s *Service) runningByAgent(ctx context.Context, repo *domain.Repository) map[string]int {
	out := map[string]int{}
	if s.d.Sessions == nil {
		return out
	}
	names := map[string]string{}
	if s.d.Agents != nil {
		if list, err := s.d.Agents.ListAgents(ctx); err == nil {
			for _, a := range list {
				names[a.ID] = a.Name
			}
		}
	}
	sessions, err := s.d.Sessions.Query(ctx, ports.SessionFilter{ProjectID: repo.ProjectID, Statuses: runningStatuses})
	if err != nil {
		return out
	}
	for _, sess := range sessions {
		if sess.RepositoryID != repo.ID || sess.AgentID == "" {
			continue
		}
		name := sess.AgentID
		if n, ok := names[name]; ok {
			name = n
		}
		out[name]++
	}
	return out
}

// UnassignAgent puts the agent on the repository's list.
func (s *Service) UnassignAgent(ctx context.Context, repositoryID, agent, listVersion string) (*domain.UnassignedAnswer, error) {
	v, err := s.edit(ctx, repositoryID, listVersion, []string{agent}, func(l *domain.RepositoryAgentList) bool {
		if l.Has(agent) {
			return false
		}
		l.Agents = append(l.Agents, agent)
		return true
	})
	if err != nil {
		return nil, err
	}
	running := 0
	for _, r := range v.Agents {
		if r.Name == agent {
			running = r.RunningSessions
		}
	}
	return &domain.UnassignedAnswer{RepositoryAgentsView: *v, RunningSessions: running}, nil
}

// AssignAgentAgain takes the agent off the repository's list.
func (s *Service) AssignAgentAgain(ctx context.Context, repositoryID, agent, listVersion string) (*domain.RepositoryAgentsView, error) {
	return s.edit(ctx, repositoryID, listVersion, []string{agent}, func(l *domain.RepositoryAgentList) bool {
		i := slices.Index(l.Agents, agent)
		if i < 0 {
			return false
		}
		l.Agents = slices.Delete(l.Agents, i, i+1)
		return true
	})
}

// AssignAllAgentsAgain empties the repository's list.
func (s *Service) AssignAllAgentsAgain(ctx context.Context, repositoryID, listVersion string) (*domain.RepositoryAgentsView, error) {
	return s.edit(ctx, repositoryID, listVersion, nil, func(l *domain.RepositoryAgentList) bool {
		if len(l.Agents) == 0 {
			return false
		}
		l.Agents = []string{}
		return true
	})
}

// edit checks the version, applies change to the list and stores it with a new
// version when change says it changed something, and announces the write.
// agents names the agent a write is about, and is checked against the catalog;
// nil skips the check.
func (s *Service) edit(ctx context.Context, repositoryID, listVersion string, agents []string, change func(*domain.RepositoryAgentList) bool) (*domain.RepositoryAgentsView, error) {
	repo, proj, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	for _, a := range agents {
		if err := s.known(a); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.d.Lists.Get(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	if list.Version != listVersion {
		current, err := s.view(ctx, repo, proj)
		if err != nil {
			return nil, err
		}
		return nil, domain.NewCatalogError("REPOSITORY_AGENTS_CHANGED",
			"REPOSITORY_AGENTS_CHANGED: the agents of repository "+repo.Name+" changed since you read them",
			map[string]any{"current": current})
	}
	before := slices.Clone(list.Agents)
	if change(list) {
		version, err := newVersion()
		if err != nil {
			return nil, err
		}
		list.Version = version
		if err := s.d.Lists.Put(ctx, list); err != nil {
			return nil, err
		}
		s.feed.announce(domain.RepositoryAgentsChanged{ProjectID: repo.ProjectID, RepositoryID: repo.ID, Agents: changedNames(before, list.Agents)})
	}
	return s.view(ctx, repo, proj)
}

// changedNames are the agents on one list and not the other, in list order.
func changedNames(a, b []string) []string {
	out := []string{}
	for _, n := range a {
		if !slices.Contains(b, n) {
			out = append(out, n)
		}
	}
	for _, n := range b {
		if !slices.Contains(a, n) {
			out = append(out, n)
		}
	}
	return out
}

// known is AGENT_NOT_FOUND unless the catalog has the agent.
func (s *Service) known(agent string) error {
	cat, err := s.d.Catalog.ReadCatalog()
	if err != nil {
		return err
	}
	for _, a := range cat.Agents {
		if a.Name == agent {
			return nil
		}
	}
	return notFound("AGENT_NOT_FOUND", "agent "+agent+" not found")
}

// FollowRepositoryAgents delivers each change until ctx ends.
func (s *Service) FollowRepositoryAgents(ctx context.Context) (<-chan domain.RepositoryAgentsChanged, error) {
	return s.feed.follow(ctx), nil
}

func newVersion() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

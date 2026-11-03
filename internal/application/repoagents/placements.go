package repoagents

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentPlacements = (*Service)(nil)

// repositoriesOf are the repositories of one project, or of every project.
func (s *Service) repositoriesOf(ctx context.Context, projectID string) []*domain.Repository {
	if projectID != "" {
		return s.d.Repositories.ListByProject(ctx, projectID)
	}
	var out []*domain.Repository
	for _, p := range s.d.Projects.List(ctx) {
		out = append(out, s.d.Repositories.ListByProject(ctx, p.ID)...)
	}
	return out
}

// UnassignedFrom names, for each agent, the repositories it is unassigned from.
func (s *Service) UnassignedFrom(ctx context.Context, projectID string) (map[string][]domain.RepositoryRef, error) {
	out := map[string][]domain.RepositoryRef{}
	for _, repo := range s.repositoriesOf(ctx, projectID) {
		list, err := s.d.Lists.Get(ctx, repo.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range list.Agents {
			out[a] = append(out[a], domain.RepositoryRef{RepositoryID: repo.ID, RepositoryName: repo.Name})
		}
	}
	return out, nil
}

// AgentStates is every catalog agent's standing in the repository.
func (s *Service) AgentStates(ctx context.Context, repositoryID string) (map[string]domain.AgentRepositoryState, error) {
	repo, proj, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	list, err := s.d.Lists.Get(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	cat, err := s.d.Catalog.ReadCatalog()
	if err != nil {
		return nil, err
	}
	prompts, err := s.digests(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]domain.AgentRepositoryState{}
	for _, a := range cat.Agents {
		state := domain.AgentAssigned
		switch {
		case !proj.AgentEnabled(a.Name):
			state = domain.AgentNotEnabled
		case list.Has(a.Name):
			state = domain.AgentUnassigned
		}
		out[a.Name] = domain.AgentRepositoryState{State: state, PromptSet: prompts[a.Name].Set}
	}
	return out, nil
}

// DecorateCatalog fills each agent's repositories: those of every project that
// enables it. The view passed in is not changed.
func (s *Service) DecorateCatalog(ctx context.Context, view *domain.CatalogView) (*domain.CatalogView, error) {
	type standing struct {
		list    *domain.RepositoryAgentList
		prompts map[string]domain.RepositoryPromptDigest
	}
	byRepo := map[string]standing{}
	for _, p := range s.d.Projects.List(ctx) {
		for _, repo := range s.d.Repositories.ListByProject(ctx, p.ID) {
			list, err := s.d.Lists.Get(ctx, repo.ID)
			if err != nil {
				return nil, err
			}
			prompts, err := s.digests(ctx, repo.ID)
			if err != nil {
				return nil, err
			}
			byRepo[repo.ID] = standing{list, prompts}
		}
	}
	out := *view
	out.Agents = make([]domain.CatalogAgent, len(view.Agents))
	copy(out.Agents, view.Agents)
	projects := s.d.Projects.List(ctx)
	for i := range out.Agents {
		a := &out.Agents[i]
		a.Repositories = []domain.CatalogAgentRepository{}
		for _, p := range projects {
			if !p.AgentEnabled(a.Name) {
				continue
			}
			for _, repo := range s.d.Repositories.ListByProject(ctx, p.ID) {
				st := byRepo[repo.ID]
				a.Repositories = append(a.Repositories, domain.CatalogAgentRepository{
					ProjectID: p.ID, RepositoryID: repo.ID, RepositoryName: repo.Name,
					Unassigned: st.list.Has(a.Name), PromptSet: st.prompts[a.Name].Set, ListVersion: st.list.Version,
				})
			}
		}
	}
	return &out, nil
}

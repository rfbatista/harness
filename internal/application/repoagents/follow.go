package repoagents

import (
	"context"
	"slices"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentRenames = (*Service)(nil)

// renamedFor is how long a renamed agent shows its previous name.
const renamedFor = 7 * 24 * time.Hour

// renamedFrom is an agent's name before a rename, and when it was renamed.
type renamedFrom struct {
	name string
	at   time.Time
}

// Subscribe forgets what a deleted repository, or a deleted project's
// repositories, kept.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.RepositoryDeleted) error {
		if err := s.d.Lists.DeleteByRepository(ctx, ev.RepositoryID); err != nil {
			return err
		}
		if err := s.d.Prompts.DeleteByRepository(ctx, ev.RepositoryID); err != nil {
			return err
		}
		s.feed.announce(domain.RepositoryAgentsChanged{ProjectID: ev.ProjectID, RepositoryID: ev.RepositoryID, Agents: []string{}})
		return nil
	})
	ports.On(sub, func(ctx context.Context, _ domain.ProjectDeleted) error {
		if err := s.d.Lists.DeleteOrphans(ctx); err != nil {
			return err
		}
		return s.d.Prompts.DeleteOrphans(ctx)
	})
}

// AgentRenamed moves the agent's place in the unassigned lists and its
// prompts to the new name. A list that changes gets a new version.
func (s *Service) AgentRenamed(ctx context.Context, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	touched := map[string]bool{}
	for _, repo := range s.repositoriesOf(ctx, "") {
		list, err := s.d.Lists.Get(ctx, repo.ID)
		if err != nil {
			return err
		}
		i := slices.Index(list.Agents, from)
		if i < 0 {
			continue
		}
		list.Agents[i] = to
		if err := s.bump(ctx, list); err != nil {
			return err
		}
		touched[repo.ID] = true
	}
	moved, err := s.d.Prompts.Rename(ctx, from, to)
	if err != nil {
		return err
	}
	for _, id := range moved {
		touched[id] = true
	}
	s.renamed[to] = renamedFrom{name: from, at: time.Now()}
	return s.announceMany(ctx, touched, []string{from, to}, len(moved) > 0)
}

// AgentDeleted removes the agent from the unassigned lists and deletes its
// prompts. A list that changes gets a new version.
func (s *Service) AgentDeleted(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	touched := map[string]bool{}
	for _, repo := range s.repositoriesOf(ctx, "") {
		list, err := s.d.Lists.Get(ctx, repo.ID)
		if err != nil {
			return err
		}
		i := slices.Index(list.Agents, name)
		if i < 0 {
			continue
		}
		list.Agents = slices.Delete(list.Agents, i, i+1)
		if err := s.bump(ctx, list); err != nil {
			return err
		}
		touched[repo.ID] = true
	}
	gone, err := s.d.Prompts.DeleteAgent(ctx, name)
	if err != nil {
		return err
	}
	for _, id := range gone {
		touched[id] = true
	}
	delete(s.renamed, name)
	return s.announceMany(ctx, touched, []string{name}, len(gone) > 0)
}

// bump stores the list under a new version.
func (s *Service) bump(ctx context.Context, list *domain.RepositoryAgentList) error {
	v, err := newVersion()
	if err != nil {
		return err
	}
	list.Version = v
	return s.d.Lists.Put(ctx, list)
}

// announceMany tells the feed, and the virtual shells when prompts moved, about
// each repository a rename or delete touched.
func (s *Service) announceMany(ctx context.Context, repos map[string]bool, agents []string, promptsMoved bool) error {
	for id := range repos {
		repo := s.d.Repositories.Get(ctx, id)
		if repo == nil {
			continue
		}
		s.feed.announce(domain.RepositoryAgentsChanged{ProjectID: repo.ProjectID, RepositoryID: id, Agents: slices.Clone(agents)})
		if promptsMoved && s.d.Events != nil {
			if err := s.d.Events.Publish(ctx, domain.RepositoryPromptsChanged{RepositoryID: id, ProjectID: repo.ProjectID}); err != nil {
				return err
			}
		}
	}
	return nil
}

// previousName is the name the agent had before a rename in the last 7 days.
func (s *Service) previousName(name string) string {
	r, ok := s.renamed[name]
	if !ok || time.Since(r.at) > renamedFor {
		return ""
	}
	return r.name
}

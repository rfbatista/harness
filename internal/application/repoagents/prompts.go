package repoagents

import (
	"context"
	"strings"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RepositoryPrompts = (*Service)(nil)

// summaryLength is how much of a prompt's first line a tab row shows.
const summaryLength = 120

// digest is a tab row's view of a prompt.
func digest(p *domain.RepositoryPrompt) domain.RepositoryPromptDigest {
	if p == nil || strings.TrimSpace(p.Content) == "" {
		return domain.RepositoryPromptDigest{}
	}
	first, _, _ := strings.Cut(strings.TrimSpace(p.Content), "\n")
	if r := []rune(first); len(r) > summaryLength {
		first = string(r[:summaryLength])
	}
	return domain.RepositoryPromptDigest{Set: true, Summary: first, Length: utf8.RuneCountInString(p.Content), Version: p.Version}
}

// digests are the repository's prompts by agent name.
func (s *Service) digests(ctx context.Context, repositoryID string) (map[string]domain.RepositoryPromptDigest, error) {
	out := map[string]domain.RepositoryPromptDigest{}
	if s.d.Prompts == nil {
		return out, nil
	}
	list, err := s.d.Prompts.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		out[p.Agent] = digest(p)
	}
	return out, nil
}

// GetRepositoryPrompt reads one agent's prompt in a repository.
func (s *Service) GetRepositoryPrompt(ctx context.Context, repositoryID, agent string) (*domain.RepositoryPromptAnswer, error) {
	repo, _, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if err := s.known(agent); err != nil {
		return nil, err
	}
	return s.promptAnswer(ctx, repo.ID, agent)
}

func (s *Service) promptAnswer(ctx context.Context, repositoryID, agent string) (*domain.RepositoryPromptAnswer, error) {
	p, err := s.d.Prompts.Get(ctx, repositoryID, agent)
	if err != nil {
		return nil, err
	}
	return &domain.RepositoryPromptAnswer{
		Agent: agent, RepositoryID: repositoryID, Content: p.Content, Version: p.Version,
		Limit: domain.RepositoryPromptLimit, Unassigned: s.IsUnassigned(ctx, repositoryID, agent),
	}, nil
}

// SaveRepositoryPrompt stores the prompt; blank content removes the entry. A
// save on an outdated version applies nothing.
func (s *Service) SaveRepositoryPrompt(ctx context.Context, repositoryID, agent, content, version string) (*domain.RepositoryPromptAnswer, error) {
	repo, _, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if err := s.known(agent); err != nil {
		return nil, err
	}
	if n := utf8.RuneCountInString(content); n > domain.RepositoryPromptLimit {
		return nil, domain.NewCatalogError("REPOSITORY_PROMPT_TOO_LONG",
			"REPOSITORY_PROMPT_TOO_LONG: a repository prompt holds at most 8000 characters",
			map[string]any{"limit": domain.RepositoryPromptLimit, "length": n})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.d.Prompts.Get(ctx, repo.ID, agent)
	if err != nil {
		return nil, err
	}
	if stored.Version != version {
		current, err := s.promptAnswer(ctx, repo.ID, agent)
		if err != nil {
			return nil, err
		}
		return nil, domain.NewCatalogError("REPOSITORY_PROMPT_CHANGED",
			"REPOSITORY_PROMPT_CHANGED: the prompt of "+agent+" in repository "+repo.Name+" changed since you read it",
			map[string]any{"current": current})
	}
	if strings.TrimSpace(content) == "" {
		if err := s.d.Prompts.Delete(ctx, repo.ID, agent); err != nil {
			return nil, err
		}
	} else {
		v, err := newVersion()
		if err != nil {
			return nil, err
		}
		if err := s.d.Prompts.Put(ctx, &domain.RepositoryPrompt{RepositoryID: repo.ID, Agent: agent, Content: content, Version: v}); err != nil {
			return nil, err
		}
	}
	s.feed.announce(domain.RepositoryAgentsChanged{ProjectID: repo.ProjectID, RepositoryID: repo.ID, Agents: []string{agent}})
	if s.d.Events != nil {
		if err := s.d.Events.Publish(ctx, domain.RepositoryPromptsChanged{RepositoryID: repo.ID, ProjectID: repo.ProjectID}); err != nil {
			return nil, err
		}
	}
	return s.promptAnswer(ctx, repo.ID, agent)
}

// PreviewRepositoryPrompt builds what the agent would receive, with the same
// function a session start uses.
func (s *Service) PreviewRepositoryPrompt(ctx context.Context, repositoryID, agent, content string) (*domain.RepositoryPromptPreview, error) {
	repo, _, err := s.repository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if err := s.known(agent); err != nil {
		return nil, err
	}
	return &domain.RepositoryPromptPreview{
		AgentPrompt:       s.agentPrompt(ctx, agent),
		RepositorySection: domain.RepositoryPromptSection(repo.Name, content),
		Heading:           domain.RepositoryPromptHeading(repo.Name),
	}, nil
}

// agentPrompt is the agent's own prompt, "" when it has none.
func (s *Service) agentPrompt(ctx context.Context, name string) string {
	if s.d.Agents == nil {
		return ""
	}
	list, err := s.d.Agents.ListAgents(ctx)
	if err != nil {
		return ""
	}
	for _, a := range list {
		if a.Name == name && a.Prompt != nil {
			return a.Prompt.Content
		}
	}
	return ""
}

var _ ports.RepositoryPromptSections = (*Service)(nil)

// RepositoryPromptSection is the text a session of the agent in the repository
// appends after the agent's own prompt, built by domain.RepositoryPromptSection.
func (s *Service) RepositoryPromptSection(ctx context.Context, repositoryID, agent string) string {
	repo := s.d.Repositories.Get(ctx, repositoryID)
	if repo == nil {
		return ""
	}
	p, err := s.d.Prompts.Get(ctx, repositoryID, agent)
	if err != nil {
		return ""
	}
	return domain.RepositoryPromptSection(repo.Name, p.Content)
}

var _ ports.RepositoryPromptFileReader = (*Service)(nil)

// ListRepositoryPromptFiles lists the prompts that are set in the project's
// repositories, for the files the virtual shell shows.
func (s *Service) ListRepositoryPromptFiles(ctx context.Context, projectID string) ([]ports.RepositoryPromptFile, error) {
	var out []ports.RepositoryPromptFile
	for _, repo := range s.d.Repositories.ListByProject(ctx, projectID) {
		list, err := s.d.Prompts.ListByRepository(ctx, repo.ID)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			out = append(out, ports.RepositoryPromptFile{
				RepositoryID: repo.ID, Repository: repo.Name, Agent: p.Agent, Version: p.Version, Content: []byte(p.Content),
			})
		}
	}
	return out, nil
}

package catalogstore

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Agents is the agents context's repository, over the catalog. It implements
// ports.AgentRepository.
type Agents struct{ cat editor }

var _ ports.AgentRepository = (*Agents)(nil)

// NewAgents is the agent repository over the catalog.
func NewAgents(cat ports.CatalogEditor) *Agents { return &Agents{cat: cat} }

// UnlinksItself says that deleting a skill or an MCP server of the catalog
// already removes it from every agent, in the same change.
func (r *Agents) UnlinksItself() bool { return true }

// domainAgent is a catalog agent as the legacy shape holds it: its resolved
// skills and its servers, by name. Its aliases are not part of the shape.
func domainAgent(a domain.CatalogAgent) *domain.Agent {
	return &domain.Agent{
		ID: a.Name, Name: a.Name, Description: a.Description,
		SkillIDs:     append([]string(nil), a.ResolvedSkills...),
		MCPServerIDs: append([]string(nil), a.MCPServers...),
		AllowedTools: append([]string(nil), a.AllowedTools...),
	}
}

func (r *Agents) find(id string) (*domain.CatalogAgent, error) {
	view, err := r.cat.ReadCatalog()
	if err != nil {
		return nil, err
	}
	for i := range view.Agents {
		if view.Agents[i].Name == id {
			return &view.Agents[i], nil
		}
	}
	return nil, nil
}

// Get returns the agent named id, or nil.
func (r *Agents) Get(id string) *domain.Agent {
	a, err := r.find(id)
	if err != nil || a == nil {
		return nil
	}
	return domainAgent(*a)
}

// List returns every agent.
func (r *Agents) List(context.Context) []*domain.Agent {
	view, err := r.cat.ReadCatalog()
	if err != nil {
		return nil
	}
	out := make([]*domain.Agent, 0, len(view.Agents))
	for _, a := range view.Agents {
		out = append(out, domainAgent(a))
	}
	return out
}

func refusePrompt(promptID string) error {
	if promptID != "" {
		return invalid("prompt_id", "an agent in the catalog has no prompt: prompts are not part of it")
	}
	return nil
}

// Create adds an agent. A prompt is refused: the catalog does not hold them.
func (r *Agents) Create(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	if err := refusePrompt(promptID); err != nil {
		return nil, err
	}
	if _, err := r.cat.SaveCatalogAgent(domain.SaveCatalogAgent{Agent: domain.CatalogAgentInput{
		Name: name, Description: description, Entries: skillIDs, MCPServers: mcpServerIDs,
	}}); err != nil {
		return nil, err
	}
	return r.Get(name), nil
}

// Update changes an agent. An empty name or description, and a skill or
// server list that is nil (left out of the request), stay as they are; an
// empty list clears. Its aliases and allowed tools always stay. A skill list
// equal to what the agent resolves to now leaves its entries alone, bundles
// included; any other list replaces them with the skills it names.
func (r *Agents) Update(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	if err := refusePrompt(promptID); err != nil {
		return nil, err
	}
	cur, err := r.find(id)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, errNotFound("AGENT_NOT_FOUND", "agent "+id)
	}
	if name == "" {
		name = cur.Name
	}
	if description == "" {
		description = cur.Description
	}
	entries := cur.Entries
	if skillIDs != nil && !sameSet(skillIDs, domainAgent(*cur).SkillIDs) {
		entries = skillIDs
	}
	if mcpServerIDs == nil {
		mcpServerIDs = cur.MCPServers
	}
	if _, err := r.cat.SaveCatalogAgent(domain.SaveCatalogAgent{PreviousName: id, Agent: domain.CatalogAgentInput{
		Name: name, Description: description, Aliases: cur.Aliases, Entries: entries,
		MCPServers: mcpServerIDs, AllowedTools: cur.AllowedTools,
	}}); err != nil {
		return nil, err
	}
	return r.Get(name), nil
}

// Delete removes an agent.
func (r *Agents) Delete(id string) error {
	_, err := r.cat.DeleteCatalogAgent(domain.DeleteCatalogItem{Name: id})
	return err
}

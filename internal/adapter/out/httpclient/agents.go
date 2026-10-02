package httpclient

import (
	"context"
	"net/url"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentCatalog = (*Agents)(nil)

// Agents is ports.AgentCatalog over the HTTP API.
type Agents struct{ c *Client }

// NewAgents returns the agents adapter over c.
func NewAgents(c *Client) *Agents { return &Agents{c: c} }

type agentOut struct {
	Agent *domain.Agent `json:"agent"`
}

type agentIn struct {
	AgentID      string   `json:"agent_id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	PromptID     string   `json:"prompt_id,omitempty"`
	SkillIDs     []string `json:"skill_ids,omitempty"`
	MCPServerIDs []string `json:"mcp_server_ids,omitempty"`
}

func (a *Agents) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	var out struct {
		Agents []*domain.Agent `json:"agents"`
	}
	return out.Agents, a.c.get(ctx, "/api/list_agents", nil, &out)
}

func (a *Agents) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	var out agentOut
	return out.Agent, a.c.get(ctx, "/api/get_agent", url.Values{"agent_id": {id}}, &out)
}

func (a *Agents) CreateAgent(ctx context.Context, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	var out agentOut
	err := a.c.post(ctx, "/api/create_agent", agentIn{
		Name: name, Description: description, PromptID: promptID, SkillIDs: skillIDs, MCPServerIDs: mcpServerIDs,
	}, &out)
	return out.Agent, err
}

func (a *Agents) UpdateAgent(ctx context.Context, id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	var out agentOut
	err := a.c.post(ctx, "/api/update_agent", agentIn{
		AgentID: id, Name: name, Description: description, PromptID: promptID, SkillIDs: skillIDs, MCPServerIDs: mcpServerIDs,
	}, &out)
	return out.Agent, err
}

func (a *Agents) DeleteAgent(ctx context.Context, id string) error {
	return a.c.post(ctx, "/api/delete_agent", map[string]string{"agent_id": id}, nil)
}

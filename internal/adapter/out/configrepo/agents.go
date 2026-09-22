package configrepo

import (
	"sort"

	"operators-mcp/internal/domain"
)

// AgentRepository implements ports.AgentRepository by reading agents.json
// live on every call. It has no PromptID/Prompt: config-sourced agents don't
// carry separate system-prompt text today, only skills + MCP servers.
// Create/Update/Delete always fail — the manifest is the source of truth,
// not a store this process writes to.
type AgentRepository struct {
	ManifestPath string
	MCPServers   *MCPServerStore
}

// NewAgentRepository returns an AgentRepository reading agents.json at
// manifestPath, resolving each agent's MCP server names via mcpServers
// (share the same *MCPServerStore instance to avoid re-parsing).
func NewAgentRepository(manifestPath string, mcpServers *MCPServerStore) *AgentRepository {
	return &AgentRepository{ManifestPath: manifestPath, MCPServers: mcpServers}
}

// List re-reads the manifest and builds every agent, in name order. An agent
// whose skills fail to resolve (unknown/cyclic bundle) is skipped rather than
// failing the whole list — one bad entry shouldn't hide every other agent.
func (r *AgentRepository) List() []*domain.Agent {
	manifest, err := LoadManifest(r.ManifestPath)
	if err != nil {
		return nil
	}
	out := make([]*domain.Agent, 0, len(manifest.Agents))
	for key := range manifest.Agents {
		if a, ok := r.build(manifest, key); ok {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get re-reads the manifest and builds one agent by its "cfg:"-prefixed id.
func (r *AgentRepository) Get(id string) *domain.Agent {
	key, ok := stripIDPrefix(id)
	if !ok {
		return nil
	}
	manifest, err := LoadManifest(r.ManifestPath)
	if err != nil {
		return nil
	}
	a, ok := r.build(manifest, key)
	if !ok {
		return nil
	}
	return a
}

func (r *AgentRepository) build(manifest *Manifest, key string) (*domain.Agent, bool) {
	cfg, ok := manifest.Agents[key]
	if !ok {
		return nil, false
	}
	skillNames, err := ResolveSkills(cfg.Skills, manifest.Bundles)
	if err != nil {
		return nil, false
	}
	skillIDs := make([]string, len(skillNames))
	for i, name := range skillNames {
		skillIDs[i] = IDPrefix + name
	}
	var mcpServerIDs []string
	for _, name := range r.MCPServers.serverNamesFor(key, cfg) {
		mcpServerIDs = append(mcpServerIDs, IDPrefix+name)
	}
	return &domain.Agent{
		ID:           IDPrefix + key,
		Name:         key,
		Description:  cfg.Description,
		SkillIDs:     skillIDs,
		MCPServerIDs: mcpServerIDs,
	}, true
}

func (r *AgentRepository) Create(string, string, string, []string, []string) (*domain.Agent, error) {
	return nil, errReadOnly("agent")
}

func (r *AgentRepository) Update(string, string, string, string, []string, []string) (*domain.Agent, error) {
	return nil, errReadOnly("agent")
}

func (r *AgentRepository) Delete(string) error { return errReadOnly("agent") }

package domain

// Agent represents an agent that can be assigned to a zone.
// Prompt, Skills, and MCPServers are populated on read; PromptID, SkillIDs, and MCPServerIDs are persisted references.
type Agent struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	PromptID     string      `json:"prompt_id,omitempty"`
	SkillIDs     []string    `json:"skill_ids,omitempty"`
	MCPServerIDs []string    `json:"mcp_server_ids,omitempty"`
	Prompt       *Prompt     `json:"prompt,omitempty"`      // populated on read, nil on write
	Skills       []Skill     `json:"skills,omitempty"`      // populated on read, empty/nil on write
	MCPServers   []MCPServer `json:"mcp_servers,omitempty"` // populated on read, empty/nil on write
}

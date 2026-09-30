package domain

// Event is a domain event: something that happened in one bounded context that
// another may have to react to. Events carry ids and values, never entities, so
// a subscriber reads what it needs through its own ports.
type Event interface {
	// EventName is the stable name subscribers register for.
	EventName() string
}

// ProjectDeleted: a project and its repositories are gone. Everything else
// scoped to the project must go with it.
type ProjectDeleted struct{ ProjectID string }

// PromptDeleted: a prompt is gone. Whatever referenced it drops the reference.
type PromptDeleted struct{ PromptID string }

// AgentDeleted: an agent is gone. Whatever referenced it drops the reference.
type AgentDeleted struct{ AgentID string }

// SkillDeleted: a skill is being deleted. It is published before the row is
// removed, and a subscriber error cancels the delete, so no reference to it
// can outlive it.
type SkillDeleted struct{ SkillID string }

// MCPServerDeleted: an MCP server is being deleted, with the same fail-closed
// ordering as SkillDeleted.
type MCPServerDeleted struct{ ServerID string }

// SettingsChanged: one setting now has a different value.
type SettingsChanged struct {
	Key string
	Old string
	New string
}

func (ProjectDeleted) EventName() string   { return "project.deleted" }
func (PromptDeleted) EventName() string    { return "prompt.deleted" }
func (AgentDeleted) EventName() string     { return "agent.deleted" }
func (SkillDeleted) EventName() string     { return "skill.deleted" }
func (MCPServerDeleted) EventName() string { return "mcp_server.deleted" }
func (SettingsChanged) EventName() string  { return "settings.changed" }

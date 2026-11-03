package domain

import "strings"

// RepositoryAgentList is a repository's list of unassigned agents (ADR 0061),
// named by catalog name, with the stored version that guards a write. The
// list is empty by default: every enabled agent is assigned.
type RepositoryAgentList struct {
	RepositoryID string
	Agents       []string
	// Version is opaque and changes on every write to the list.
	Version string
}

// Has says whether the agent is on the list.
func (l *RepositoryAgentList) Has(agent string) bool {
	if l == nil {
		return false
	}
	for _, a := range l.Agents {
		if a == agent {
			return true
		}
	}
	return false
}

// States of an agent in a repository's Agents tab.
const (
	AgentAssigned   = "assigned"
	AgentUnassigned = "unassigned"
	AgentNotEnabled = "not_enabled"
)

// RepositoryAgentsView is the read behind a repository's Agents tab
// (get_repository_agents).
type RepositoryAgentsView struct {
	Repository  RepositoryAgentsRepo    `json:"repository"`
	Project     RepositoryAgentsProject `json:"project"`
	ListVersion string                  `json:"list_version"`
	Agents      []RepositoryAgentRow    `json:"agents"`
}

// RepositoryAgentsRepo names the repository of the view.
type RepositoryAgentsRepo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RepositoryAgentsProject is the project's agent setting at a glance.
type RepositoryAgentsProject struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	AgentsMode   string `json:"agents_mode"`
	EnabledCount int    `json:"enabled_count"`
	CatalogCount int    `json:"catalog_count"`
}

// RepositoryAgentRow is one agent in the Agents tab.
type RepositoryAgentRow struct {
	Name            string                 `json:"name"`
	Description     string                 `json:"description"`
	State           string                 `json:"state"`
	Unassigned      bool                   `json:"unassigned"`
	Prompt          RepositoryPromptDigest `json:"prompt"`
	RunningSessions int                    `json:"running_sessions"`
	PreviousName    string                 `json:"previous_name,omitempty"`
}

// RepositoryPromptDigest summarises a repository prompt on a tab row.
type RepositoryPromptDigest struct {
	Set     bool   `json:"set"`
	Summary string `json:"summary"`
	Length  int    `json:"length"`
	Version string `json:"version"`
}

// RepositoryAgentsChanged says a repository's assignments or prompts changed
// (the repository_agents_changed event).
type RepositoryAgentsChanged struct {
	ProjectID    string   `json:"project_id"`
	RepositoryID string   `json:"repository_id"`
	Agents       []string `json:"agents"`
}

// UnassignedAnswer is unassign_agent's answer: the view plus how many of the
// agent's sessions still run in the repository.
type UnassignedAnswer struct {
	RepositoryAgentsView
	RunningSessions int `json:"running_sessions"`
}

// RepositoryPromptLimit is the most characters a repository prompt holds, so
// one repository cannot bloat every session's context.
const RepositoryPromptLimit = 8000

// RepositoryPrompt is the optional text one repository adds to one agent's
// own prompt (ADR 0061).
type RepositoryPrompt struct {
	RepositoryID string
	Agent        string
	Content      string
	// Version is opaque and changes on every save; "" for an entry never saved.
	Version string
}

// RepositoryPromptAnswer is the get_repository_prompt answer, and
// save_repository_prompt's.
type RepositoryPromptAnswer struct {
	Agent        string `json:"agent"`
	RepositoryID string `json:"repository_id"`
	Content      string `json:"content"`
	Version      string `json:"version"`
	Limit        int    `json:"limit"`
	Unassigned   bool   `json:"unassigned"`
}

// RepositoryPromptPreview is what preview_repository_prompt answers: the
// agent's own prompt, the exact text appended after it, and its heading.
type RepositoryPromptPreview struct {
	AgentPrompt       string `json:"agent_prompt"`
	RepositorySection string `json:"repository_section"`
	Heading           string `json:"heading"`
}

// RepositoryPromptHeading is the heading of a repository's section of an
// agent's system prompt.
func RepositoryPromptHeading(repositoryName string) string {
	return "## Repository prompt: " + repositoryName
}

// RepositoryPromptSection is the text appended after the agent's own prompt:
// the heading, then the prompt. It is "" for a blank prompt. The one place it
// is built: session starts, resumes, the preview and the shell file all use it.
func RepositoryPromptSection(repositoryName, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return RepositoryPromptHeading(repositoryName) + "\n\n" + content
}

// RepositoryRef names a repository.
type RepositoryRef struct {
	RepositoryID   string `json:"repository_id"`
	RepositoryName string `json:"repository_name"`
}

// AgentRepositoryState is an agent's standing in one repository: its state
// (assigned, unassigned or not enabled) and whether a repository prompt is set.
type AgentRepositoryState struct {
	State     string
	PromptSet bool
}

// NotOfferedAgent is a catalog agent a repository does not offer to a new
// session, with why: "unassigned" or "not_enabled".
type NotOfferedAgent struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

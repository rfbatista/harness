package domain

import (
	"encoding/json"
	"slices"
)

// Mode agents (ADR 0103): one server-wide setting per session mode says which
// agents may start in it. All agents, the built-in, or Selected agents with a
// list in which No agent (a session that runs as no agent) is one entry.

// SettingModeAgentsModePrefix starts the settings key of each mode's agents;
// the mode value ends it, "default" for the default mode. An unset key is All
// agents.
const SettingModeAgentsModePrefix = "agents.mode."

// ModeAgentsKey is the settings key holding a mode's agents.
func ModeAgentsKey(mode SessionMode) string {
	if mode == SessionModeDefault {
		return SettingModeAgentsModePrefix + "default"
	}
	return SettingModeAgentsModePrefix + string(mode)
}

// ModeAgentsPath is the catalog change path of a mode's agents.
func ModeAgentsPath(mode SessionMode) string {
	if mode == SessionModeDefault {
		return "modes/default"
	}
	return "modes/" + string(mode)
}

// ModeAgents is one entry of the mode agents read.
type ModeAgents struct {
	Mode SessionMode `json:"mode"`
	Name string      `json:"name"`
	// State is AgentsAll or AgentsSelected. In AgentsAll, Agents is empty and
	// NoAgent is true: everything may start.
	State   string   `json:"state"`
	Agents  []string `json:"agents"`
	NoAgent bool     `json:"no_agent"`
}

// SetModeAgents replaces one mode's agents.
type SetModeAgents struct {
	Mode    string   `json:"mode"`
	State   string   `json:"state"`
	Agents  []string `json:"agents"`
	NoAgent bool     `json:"no_agent"`
}

// storedModeAgents is a Selected mode's setting value.
type storedModeAgents struct {
	Agents  []string `json:"agents"`
	NoAgent bool     `json:"no_agent"`
}

// ModeAgentsEntry reads one mode's agents through read, which returns a
// setting's stored value ("" while unset). A value that does not parse is All
// agents: a mode never closes by accident. A mode that is not in the mode
// list is Default.
func ModeAgentsEntry(mode SessionMode, read func(key string) string) ModeAgents {
	if !slices.Contains(modeModelModes, mode) {
		mode = SessionModeDefault
	}
	all := ModeAgents{Mode: mode, Name: ModeName(mode), State: AgentsAll, Agents: []string{}, NoAgent: true}
	raw := read(ModeAgentsKey(mode))
	if raw == "" {
		return all
	}
	var s storedModeAgents
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return all
	}
	if s.Agents == nil {
		s.Agents = []string{}
	}
	return ModeAgents{Mode: mode, Name: all.Name, State: AgentsSelected, Agents: s.Agents, NoAgent: s.NoAgent}
}

// ModeAgentsList reads every mode's agents, in the order a person reads the
// modes.
func ModeAgentsList(read func(key string) string) []ModeAgents {
	out := make([]ModeAgents, 0, len(modeModelModes))
	for _, mode := range modeModelModes {
		out = append(out, ModeAgentsEntry(mode, read))
	}
	return out
}

// Allows says whether a session may start in the mode as the agent named
// agent, or as no agent when agent is "".
func (m ModeAgents) Allows(agent string) bool {
	if m.State != AgentsSelected {
		return true
	}
	if agent == "" {
		return m.NoAgent
	}
	return slices.Contains(m.Agents, agent)
}

// ModeAgentsValue is the setting value that stores the entry: "" for All
// agents, JSON for Selected agents.
func ModeAgentsValue(state string, agents []string, noAgent bool) string {
	if state != AgentsSelected {
		return ""
	}
	if agents == nil {
		agents = []string{}
	}
	b, _ := json.Marshal(storedModeAgents{Agents: agents, NoAgent: noAgent})
	return string(b)
}

// ModeAgentNotAvailableMessage is what a refused start tells its caller. The
// code leads the message: MCP clients see only the message. agent is "" for a
// session with no agent.
func ModeAgentNotAvailableMessage(modeName, agent string) string {
	who := "agent " + agent
	if agent == "" {
		who = "a session with no agent"
	}
	return "AGENT_NOT_AVAILABLE_IN_MODE: " + who + " cannot start in " + modeName +
		" sessions. Only the person can change which agents a mode accepts, in the server settings."
}

func validateModeAgentsSetting(key, value string) error {
	if value == "" {
		return nil
	}
	var s storedModeAgents
	if err := json.Unmarshal([]byte(value), &s); err != nil {
		return &StructuredError{Code: "INVALID_INPUT", Message: key + " must be the mode agents JSON"}
	}
	return nil
}

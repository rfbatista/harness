package agentcatalog

import (
	"context"

	"operators-mcp/internal/domain"
)

// ModeAgentSettings is where the catalog keeps the mode agents (ADR 0103):
// the server's settings, one key per session mode.
type ModeAgentSettings interface {
	Setting(key string) string
	UpdateSettings(values map[string]string) (map[string]string, error)
}

// WithModeAgents gives the catalog the settings that hold the mode agents.
// Without it every mode is All agents and a write is refused.
func WithModeAgents(s ModeAgentSettings) Option { return func(c *Catalog) { c.modes = s } }

// UseModeAgents gives the catalog its mode agent settings after it was built,
// for wiring.
func (c *Catalog) UseModeAgents(s ModeAgentSettings) { c.modes = s }

func (c *Catalog) readMode(key string) string {
	if c.modes == nil {
		return ""
	}
	return c.modes.Setting(key)
}

// GetModeAgents reads every mode's agents, in the order of the session modes.
func (c *Catalog) GetModeAgents(context.Context) ([]domain.ModeAgents, error) {
	return domain.ModeAgentsList(c.readMode), nil
}

// SetModeAgents replaces one mode's agents. Each name must be a catalog
// agent. In all state the list and the flag are ignored.
func (c *Catalog) SetModeAgents(_ context.Context, in domain.SetModeAgents) (*domain.ModeAgents, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	mode, err := domain.ParseSessionMode(in.Mode)
	if err != nil {
		return nil, invalid("mode", `mode is "", "architect", "implement", "design", "product", "designsystem", "review", "test", "qa" or "operate"`)
	}
	var names []string
	switch in.State {
	case domain.AgentsAll:
	case domain.AgentsSelected:
		l, err := c.load()
		if err != nil {
			return nil, err
		}
		var unknown []string
		seen := map[string]bool{}
		names = []string{}
		for _, n := range in.Agents {
			if l.agents.get("agents").get(n) == nil {
				unknown = append(unknown, n)
			}
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		if len(unknown) > 0 {
			return nil, unknownRefs(unknown)
		}
	default:
		return nil, invalid("state", "state is all or selected")
	}
	if c.modes == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "mode agents are not available"}
	}
	value := domain.ModeAgentsValue(in.State, names, in.NoAgent)
	if _, err := c.modes.UpdateSettings(map[string]string{domain.ModeAgentsKey(mode): value}); err != nil {
		return nil, err
	}
	c.last = nil // the latest change is no longer the latest write
	c.publish([]string{domain.ModeAgentsPath(mode)})
	entry := domain.ModeAgentsEntry(mode, c.readMode)
	return &entry, nil
}

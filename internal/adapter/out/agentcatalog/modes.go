package agentcatalog

import (
	"context"

	"operators-mcp/internal/domain"
)

// ModeAgentSettings is where the catalog keeps the mode agents (ADR 0103):
// the server's settings, one key per session mode.
type ModeAgentSettings interface {
	Setting(key string) string
	UpdateModeAgentSettings(values map[string]string) (map[string]string, error)
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
	if c.modes == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "mode agents are not available"}
	}
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
	value := domain.ModeAgentsValue(in.State, names, in.NoAgent)
	if _, err := c.modes.UpdateModeAgentSettings(map[string]string{domain.ModeAgentsKey(mode): value}); err != nil {
		return nil, err
	}
	c.last = nil // the latest change is no longer the latest write
	c.publish([]string{domain.ModeAgentsPath(mode)})
	entry := domain.ModeAgentsEntry(mode, c.readMode)
	return &entry, nil
}

// modeEdit is the list of a Selected mode a change sets. Only the list moves:
// the No agent flag stays as it is.
type modeEdit struct {
	mode          domain.SessionMode
	before, after []string
}

func (m *modeEdit) changed() bool { return !sameNames(m.before, m.after) }

// modeChanges are the edits as the answer shows them, and marks their paths
// as touched.
func modeChanges(edits []*modeEdit, keys map[string]bool) []domain.CatalogModeChange {
	out := []domain.CatalogModeChange{}
	for _, m := range edits {
		out = append(out, domain.CatalogModeChange{Mode: m.mode, Name: domain.ModeName(m.mode), Before: nonNil(m.before), After: nonNil(m.after)})
		keys[domain.ModeAgentsPath(m.mode)] = true
	}
	return out
}

// dropUnchangedModes forgets the edits that end where they began.
func (t *txn) dropUnchangedModes() {
	kept := t.modes[:0]
	for _, m := range t.modes {
		if m.changed() {
			kept = append(kept, m)
		}
	}
	t.modes = kept
}

// selectedModes are the Selected modes, as they are now.
func (c *Catalog) selectedModes() []domain.ModeAgents {
	var out []domain.ModeAgents
	for _, m := range domain.ModeAgentsList(c.readMode) {
		if m.State == domain.AgentsSelected {
			out = append(out, m)
		}
	}
	return out
}

// modeEditOf is the write's edit of a mode's list, made when there is none.
func (t *txn) modeEditOf(m domain.ModeAgents) *modeEdit {
	for _, e := range t.modes {
		if e.mode == m.Mode {
			return e
		}
	}
	e := &modeEdit{mode: m.Mode, before: append([]string{}, m.Agents...), after: append([]string{}, m.Agents...)}
	t.modes = append(t.modes, e)
	return e
}

// moveModeAgent makes every Selected mode that lists from list to instead,
// or drop it when to is "".
func (t *txn) moveModeAgent(from, to string) {
	for _, m := range t.c.selectedModes() {
		cur := t.modeEditOf(m)
		if !contains(cur.after, from) {
			continue
		}
		if to == "" {
			cur.after = without(cur.after, from)
		} else {
			cur.after = replaceName(cur.after, from, to)
		}
	}
}

// setAgentModes makes the Selected modes that list the agent exactly those
// of modes (mode values).
func (t *txn) setAgentModes(agent string, modes []string) {
	for _, m := range t.c.selectedModes() {
		cur := t.modeEditOf(m)
		switch wanted := contains(modes, string(m.Mode)); {
		case wanted && !contains(cur.after, agent):
			cur.after = append(append([]string{}, cur.after...), agent)
		case !wanted && contains(cur.after, agent):
			cur.after = without(cur.after, agent)
		}
	}
}

// checkAgentModes refuses a save whose modes name a mode that is not Selected.
func (c *Catalog) checkAgentModes(modes []string) error {
	selected := map[string]bool{}
	for _, m := range c.selectedModes() {
		selected[string(m.Mode)] = true
	}
	for _, v := range modes {
		mode, err := domain.ParseSessionMode(v)
		if err != nil {
			return invalid("modes", "mode "+v+" is not a session mode")
		}
		if !selected[string(mode)] {
			return invalid("modes", "mode "+domain.ModeName(mode)+" accepts all agents: change it in the server settings")
		}
	}
	return nil
}

// applyMode writes a mode edit's list, or puts the old one back.
func (c *Catalog) applyMode(m *modeEdit, forward bool) error {
	list := m.after
	if !forward {
		list = m.before
	}
	if c.modes == nil {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "mode agents are not available"}
	}
	cur := domain.ModeAgentsEntry(m.mode, c.readMode)
	value := domain.ModeAgentsValue(domain.AgentsSelected, list, cur.NoAgent)
	_, err := c.modes.UpdateModeAgentSettings(map[string]string{domain.ModeAgentsKey(m.mode): value})
	return err
}

// agentModes are the mode values where the agent may start.
func (c *Catalog) agentModes(name string) []string {
	out := []string{}
	for _, m := range domain.ModeAgentsList(c.readMode) {
		if m.Allows(name) {
			out = append(out, string(m.Mode))
		}
	}
	return out
}

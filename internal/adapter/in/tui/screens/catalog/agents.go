package catalog

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// agentsSpec lists agent profiles; n/e edit the profile, c links capabilities.
type agentsSpec struct{}

func (agentsSpec) columns() []components.Column {
	return []components.Column{{Title: "Agent", Width: 24}, {Title: "Description"}, {Title: "Capabilities", Width: 20}}
}

func (agentsSpec) entries(m *Model) []entry {
	var out []entry
	for _, a := range m.snap.Agents {
		caps := fmt.Sprintf("%s · %d MCP", plural(len(a.SkillIDs), "skill"), len(a.MCPServerIDs))
		out = append(out, entry{a.ID, []string{a.Name, a.Description, caps}})
	}
	return out
}

func (agentsSpec) summary() string   { return "Agent profiles" }
func (agentsSpec) emptyText() string { return "No agents yet. Press n to create one." }

func (agentsSpec) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "n", Desc: "new"}, {Key: "e", Desc: "edit"}, {Key: "c", Desc: "capabilities"}, {Key: "D", Desc: "delete"}}
}

func (agentsSpec) agent(m *Model, id string) *domain.Agent {
	for _, a := range m.snap.Agents {
		if a.ID == id {
			return a
		}
	}
	return nil
}

func (s agentsSpec) key(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	th, be := m.ctx.Theme, m.be
	switch k.String() {
	case "n":
		m.overlays.OpenForm(components.NewForm(th, "New agent",
			components.TextField("name", "Name", "").Required(),
			components.TextField("description", "Description", ""),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("agent.create", "Agent created", func() error {
				_, err := be.CreateAgent(v["name"], v["description"], "", nil, nil)
				return err
			})
		})
		return nil, true
	case "e", "enter":
		a := s.agent(m, m.Selected())
		if a == nil {
			return nil, true
		}
		id, promptID, skills, mcps := a.ID, a.PromptID, a.SkillIDs, a.MCPServerIDs
		m.overlays.OpenForm(components.NewForm(th, "Edit agent",
			components.TextField("name", "Name", a.Name).Required(),
			components.TextField("description", "Description", a.Description),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("agent.update", "Agent updated", func() error {
				_, err := be.UpdateAgent(id, v["name"], v["description"], promptID, skills, mcps)
				return err
			})
		})
		return nil, true
	case "c":
		a := s.agent(m, m.Selected())
		if a == nil {
			return nil, true
		}
		var skills, mcps []components.Option
		for _, sk := range m.snap.Skills {
			skills = append(skills, components.Option{Label: sk.Name, Value: sk.ID})
		}
		for _, srv := range m.snap.MCPServers {
			mcps = append(mcps, components.Option{Label: srv.Name, Value: srv.ID})
		}
		id, name, desc, promptID := a.ID, a.Name, a.Description, a.PromptID
		m.overlays.OpenForm(components.NewForm(th, "Capabilities · "+a.Name,
			components.MultiSelectField("skills", "Skills", skills, a.SkillIDs),
			components.MultiSelectField("mcps", "MCP servers", mcps, a.MCPServerIDs),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			chosenSkills, chosenMCPs := sub.Multi["skills"], sub.Multi["mcps"]
			return components.Op("agent.capabilities", "Capabilities saved", func() error {
				_, err := be.UpdateAgent(id, name, desc, promptID, chosenSkills, chosenMCPs)
				return err
			})
		})
		return nil, true
	case "D":
		a := s.agent(m, m.Selected())
		if a == nil {
			return nil, true
		}
		id := a.ID
		m.overlays.OpenConfirm(th, "Delete agent",
			fmt.Sprintf("Delete %q? Sessions already started keep running; the profile is gone for good.", a.Name),
			func() tea.Cmd {
				return components.Op("agent.delete", "Agent deleted", func() error { return be.DeleteAgent(id) })
			})
		return nil, true
	}
	return nil, false
}

func (agentsSpec) result(*Model, tea.Msg) (tea.Cmd, bool)     { return nil, false }
func (agentsSpec) conflict(*Model, components.OpDoneMsg) bool { return false }

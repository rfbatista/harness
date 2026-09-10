package tui

import (
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
)

// paletteItems flattens the snapshot into what Ctrl+K can search.
func (m Model) paletteItems() []components.PaletteItem {
	var items []components.PaletteItem
	for _, t := range m.snap.Tickets {
		items = append(items, components.PaletteItem{Kind: core.KindTask, Label: t.Title, Hint: m.snap.ProjectName(t.ProjectID), ID: t.ID})
	}
	for _, s := range m.snap.Sessions {
		items = append(items, components.PaletteItem{Kind: core.KindSession, Label: s.Task, Hint: s.Branch, ID: s.ID})
	}
	for _, a := range m.snap.Agents {
		items = append(items, components.PaletteItem{Kind: core.KindAgent, Label: a.Name, ID: a.ID})
	}
	for _, s := range m.snap.Skills {
		items = append(items, components.PaletteItem{Kind: core.KindSkill, Label: s.Name, ID: s.ID})
	}
	for _, s := range m.snap.MCPServers {
		items = append(items, components.PaletteItem{Kind: core.KindMCPServer, Label: s.Name, Hint: s.Transport, ID: s.ID})
	}
	for _, p := range m.snap.Projects {
		items = append(items, components.PaletteItem{Kind: core.KindProject, Label: p.Name, Hint: p.RootDir, ID: p.ID})
	}
	return items
}

// navigateFor maps a palette pick onto the section that owns it. A session
// pick opens the session over whatever section is showing, so esc returns
// the user to where they were.
func navigateFor(it components.PaletteItem) core.NavigateMsg {
	if it.Kind == core.KindSession {
		return core.NavigateMsg{SessionID: it.ID}
	}
	return core.NavigateMsg{Section: it.Kind.Section(), HasSection: true}
}

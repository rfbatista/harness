package catalog

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/domain"
)

// projectsSpec is the Settings section: projects plus the workspaces root.
type projectsSpec struct{}

func (projectsSpec) columns() []components.Column {
	return []components.Column{{Title: "Project", Width: 24}, {Title: "Root"}, {Title: "Repos", Width: 8}, {Title: "Contexts", Width: 9}}
}

func (projectsSpec) entries(m *Model) []entry {
	var out []entry
	for _, p := range m.snap.Projects {
		contexts := len(m.be.ListBoundedContexts(p.ID))
		out = append(out, entry{p.ID, []string{p.Name, p.RootDir, plural(len(m.snap.RepositoriesOf(p.ID)), "repo"), fmt.Sprintf("%d", contexts)}})
	}
	return out
}

func (projectsSpec) summary() string   { return "Projects" }
func (projectsSpec) emptyText() string { return "No projects yet. Press n to create one." }

func (projectsSpec) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "↵", Desc: "open"}, {Key: "n", Desc: "new"}, {Key: "e", Desc: "edit"}, {Key: "D", Desc: "delete"}, {Key: "w", Desc: "workspaces root"}}
}

func (projectsSpec) key(m *Model, k tea.KeyPressMsg) (tea.Cmd, bool) {
	th, be := m.ctx.Theme, m.be
	switch k.String() {
	case "n":
		m.overlays.OpenForm(components.NewForm(th, "New project",
			components.TextField("name", "Name", "").Required(),
			components.TextField("root_dir", "Root directory", "").Placeholder("/absolute/path"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("project.create", "Project created", func() error {
				_, err := be.CreateProject(v["name"], v["root_dir"])
				return err
			})
		})
		return nil, true
	case "e":
		p := m.snap.Project(m.Selected())
		if p == nil {
			return nil, true
		}
		id := p.ID
		m.overlays.OpenForm(components.NewForm(th, "Edit project",
			components.TextField("name", "Name", p.Name).Required(),
			components.TextField("root_dir", "Root directory", p.RootDir),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("project.update", "Project updated", func() error {
				_, err := be.UpdateProject(id, v["name"], v["root_dir"])
				return err
			})
		})
		return nil, true
	case "D":
		p := m.snap.Project(m.Selected())
		if p == nil {
			return nil, true
		}
		id := p.ID
		m.overlays.OpenConfirm(th, "Delete project",
			fmt.Sprintf("Delete %q with its repositories, zones and bounded contexts?", p.Name),
			func() tea.Cmd {
				return components.Op("project.delete", "Project deleted", func() error { return be.DeleteProject(id) })
			})
		return nil, true
	case "w":
		m.overlays.OpenForm(components.NewForm(th, "Workspaces root",
			components.TextField(domain.SettingWorkspacesRoot, "Directory session worktrees are created under", m.snap.Settings[domain.SettingWorkspacesRoot]).Placeholder("~/.coding-pool/worktrees"),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			root := sub.Values[domain.SettingWorkspacesRoot]
			return components.Op("settings.update", "Settings saved", func() error {
				_, err := be.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: root})
				return err
			})
		})
		return nil, true
	case "enter":
		p := m.snap.Project(m.Selected())
		if p == nil {
			return nil, true
		}
		m.detail = newProjectDetail(m.ctx, m.be, m.snap, p.ID)
		return nil, true
	}
	return nil, false
}

func (projectsSpec) result(*Model, tea.Msg) (tea.Cmd, bool)     { return nil, false }
func (projectsSpec) conflict(*Model, components.OpDoneMsg) bool { return false }

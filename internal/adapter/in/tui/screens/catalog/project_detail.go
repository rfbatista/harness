package catalog

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/theme"
	"operators-mcp/internal/domain"
)

// projectDetail is the drill-in for one project: three tabs for repositories,
// bounded contexts and ignored paths, each with add / edit / delete.
type projectDetail struct {
	ctx       core.Context
	be        backend.Backend
	snap      backend.Snapshot
	projectID string
	tab       int
	cursor    [3]int
	overlays  components.Overlays
}

const (
	tabRepos = iota
	tabContexts
	tabIgnored
)

var tabLabels = [3]string{"Repositories", "Bounded contexts", "Ignored paths"}

func newProjectDetail(ctx core.Context, be backend.Backend, snap backend.Snapshot, projectID string) *projectDetail {
	return &projectDetail{ctx: ctx, be: be, snap: snap, projectID: projectID}
}

func (d *projectDetail) project() *domain.Project { return d.snap.Project(d.projectID) }

func (d *projectDetail) repos() []*domain.Repository { return d.snap.RepositoriesOf(d.projectID) }

func (d *projectDetail) contexts() []*domain.BoundedContext {
	return d.be.ListBoundedContexts(d.projectID)
}

func (d *projectDetail) ignored() []string {
	if p := d.project(); p != nil {
		return p.IgnoredPaths
	}
	return nil
}

func (d *projectDetail) rowCount() int {
	switch d.tab {
	case tabRepos:
		return len(d.repos())
	case tabContexts:
		return len(d.contexts())
	}
	return len(d.ignored())
}

func (d *projectDetail) clamp() {
	n := d.rowCount()
	if d.cursor[d.tab] >= n {
		d.cursor[d.tab] = n - 1
	}
	if d.cursor[d.tab] < 0 {
		d.cursor[d.tab] = 0
	}
}

func (d *projectDetail) capturing() bool { return d.overlays.Active() }

func (d *projectDetail) hints() []core.KeyHint {
	return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "h/l", Desc: "tab"}, {Key: "n", Desc: "add"}, {Key: "e", Desc: "edit"}, {Key: "D", Desc: "delete"}, {Key: "j/k", Desc: "move"}}
}

func (d *projectDetail) update(msg tea.Msg) (detail, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		d.ctx = msg.Ctx
		return d, nil
	case core.SnapshotMsg:
		d.snap = msg.Snapshot
		if d.project() == nil {
			return d, closeDetail
		}
		d.clamp()
		return d, nil
	}
	if cmd, handled := d.overlays.Update(msg); handled {
		return d, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		cmd, _ := d.overlays.Done(msg)
		return d, cmd
	case tea.KeyPressMsg:
		return d, d.key(msg)
	}
	return d, nil
}

func (d *projectDetail) key(k tea.KeyPressMsg) tea.Cmd {
	p := d.project()
	if p == nil {
		return closeDetail
	}
	switch k.String() {
	case "esc", "q":
		return closeDetail
	case "l", "right":
		d.tab = (d.tab + 1) % 3
	case "h", "left":
		d.tab = (d.tab + 2) % 3
	case "j", "down":
		d.cursor[d.tab]++
		d.clamp()
	case "k", "up":
		d.cursor[d.tab]--
		d.clamp()
	case "n":
		d.openAdd()
	case "e":
		d.openEdit()
	case "D":
		d.openDelete(p.Name)
	}
	return nil
}

// openAdd opens the add form of the current tab.
func (d *projectDetail) openAdd() {
	th, be, pid := d.ctx.Theme, d.be, d.projectID
	switch d.tab {
	case tabRepos:
		d.overlays.OpenForm(repoForm(th, "New repository", nil), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("repo.create", "Repository added", func() error {
				_, err := be.CreateRepository(pid, v["name"], v["description"], v["url"], v["root_dir"])
				return err
			})
		})
	case tabContexts:
		d.overlays.OpenForm(contextForm(th, "New bounded context", nil), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			terms := parseTerms(v["terms"])
			return components.Op("context.create", "Bounded context created", func() error {
				_, err := be.CreateBoundedContext(pid, v["name"], v["purpose"], terms)
				return err
			})
		})
	case tabIgnored:
		d.overlays.OpenForm(components.NewForm(th, "Ignore path",
			components.TextField("path", "Path (relative to the project root)", "").Required(),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			path := sub.Values["path"]
			return components.Op("ignore.add", "Path ignored", func() error {
				_, err := be.AddIgnoredPath(pid, path)
				return err
			})
		})
	}
}

// openEdit opens the edit form for the selected row; ignored paths have none.
func (d *projectDetail) openEdit() {
	th, be := d.ctx.Theme, d.be
	switch d.tab {
	case tabRepos:
		r := d.selectedRepo()
		if r == nil {
			return
		}
		id := r.ID
		d.overlays.OpenForm(repoForm(th, "Edit repository", r), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("repo.update", "Repository updated", func() error {
				_, err := be.UpdateRepository(id, v["name"], v["description"], v["url"], v["root_dir"])
				return err
			})
		})
	case tabContexts:
		c := d.selectedContext()
		if c == nil {
			return
		}
		id := c.ID
		d.overlays.OpenForm(contextForm(th, "Edit bounded context", c), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			terms := parseTerms(v["terms"])
			return components.Op("context.update", "Bounded context updated", func() error {
				_, err := be.UpdateBoundedContext(id, v["name"], v["purpose"], terms)
				return err
			})
		})
	}
}

// openDelete asks before removing the selected row of the current tab.
func (d *projectDetail) openDelete(projectName string) {
	th, be, pid := d.ctx.Theme, d.be, d.projectID
	switch d.tab {
	case tabRepos:
		r := d.selectedRepo()
		if r == nil {
			return
		}
		id := r.ID
		d.overlays.OpenConfirm(th, "Remove repository",
			fmt.Sprintf("Remove %q from %s? The files on disk are untouched.", r.Name, projectName),
			func() tea.Cmd {
				return components.Op("repo.delete", "Repository removed", func() error { return be.DeleteRepository(id) })
			})
	case tabContexts:
		c := d.selectedContext()
		if c == nil {
			return
		}
		id := c.ID
		d.overlays.OpenConfirm(th, "Delete bounded context",
			fmt.Sprintf("Delete %q? Zones linked to this context will be unlinked.", c.Name),
			func() tea.Cmd {
				return components.Op("context.delete", "Bounded context deleted", func() error { return be.DeleteBoundedContext(id) })
			})
	case tabIgnored:
		ip := d.selectedIgnored()
		if ip == "" {
			return
		}
		d.overlays.OpenConfirm(th, "Stop ignoring", fmt.Sprintf("Stop ignoring %q?", ip), func() tea.Cmd {
			return components.Op("ignore.remove", "Path no longer ignored", func() error {
				_, err := be.RemoveIgnoredPath(pid, ip)
				return err
			})
		})
	}
}

func (d *projectDetail) selectedRepo() *domain.Repository {
	rs := d.repos()
	if i := d.cursor[tabRepos]; i >= 0 && i < len(rs) {
		return rs[i]
	}
	return nil
}

func (d *projectDetail) selectedContext() *domain.BoundedContext {
	cs := d.contexts()
	if i := d.cursor[tabContexts]; i >= 0 && i < len(cs) {
		return cs[i]
	}
	return nil
}

func (d *projectDetail) selectedIgnored() string {
	ips := d.ignored()
	if i := d.cursor[tabIgnored]; i >= 0 && i < len(ips) {
		return ips[i]
	}
	return ""
}

func repoForm(th theme.Theme, title string, r *domain.Repository) components.Form {
	var name, desc, url, root string
	if r != nil {
		name, desc, url, root = r.Name, r.Description, r.URL, r.RootDir
	}
	return components.NewForm(th, title,
		components.TextField("name", "Name", name).Required(),
		components.TextField("description", "Description", desc),
		components.TextField("url", "Remote URL", url),
		components.TextField("root_dir", "Local root directory", root).Placeholder("/absolute/path to the clone"),
	)
}

func contextForm(th theme.Theme, title string, c *domain.BoundedContext) components.Form {
	var name, purpose string
	var terms []domain.LanguageTerm
	if c != nil {
		name, purpose, terms = c.Name, c.Purpose, c.UbiquitousLanguage
	}
	return components.NewForm(th, title,
		components.TextField("name", "Name", name).Required(),
		components.TextField("purpose", "Purpose", purpose),
		components.MultilineField("terms", "Ubiquitous language (one `Term: definition` per line)", formatTerms(terms)),
	)
}

// formatTerms renders terms as editable lines; parseTerms reads them back.
func formatTerms(terms []domain.LanguageTerm) string {
	lines := make([]string, 0, len(terms))
	for _, t := range terms {
		lines = append(lines, t.Term+": "+t.Definition)
	}
	return strings.Join(lines, "\n")
}

func parseTerms(text string) []domain.LanguageTerm {
	var out []domain.LanguageTerm
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		term, def, _ := strings.Cut(line, ":")
		out = append(out, domain.LanguageTerm{Term: strings.TrimSpace(term), Definition: strings.TrimSpace(def)})
	}
	return out
}

func (d *projectDetail) view() string {
	th := d.ctx.Theme
	p := d.project()
	if p == nil {
		return ""
	}
	header := th.Title().Render(p.Name) + "  " + th.Meta().Render(p.RootDir)
	var tabs []string
	for i, label := range tabLabels {
		if i == d.tab {
			tabs = append(tabs, th.AccentText().Render("["+label+"]"))
		} else {
			tabs = append(tabs, th.Subtitle().Render(" "+label+" "))
		}
	}
	var rows []string
	switch d.tab {
	case tabRepos:
		for i, r := range d.repos() {
			line := fmt.Sprintf("%-24s %s", r.Name, th.Meta().Render(r.RootDir))
			if r.URL != "" {
				line += "  " + th.Meta().Render(r.URL)
			}
			rows = append(rows, d.row(line, i == d.cursor[tabRepos]))
		}
		if len(rows) == 0 {
			rows = append(rows, th.Subtitle().Render("No repositories linked. Press n to add one."))
		}
	case tabContexts:
		for i, c := range d.contexts() {
			line := fmt.Sprintf("%-24s %s  %s", c.Name, th.Meta().Render(plural(len(c.UbiquitousLanguage), "term")), th.Subtitle().Render(c.Purpose))
			rows = append(rows, d.row(line, i == d.cursor[tabContexts]))
		}
		if len(rows) == 0 {
			rows = append(rows, th.Subtitle().Render("No bounded contexts yet. Press n to add one."))
		}
	case tabIgnored:
		for i, ip := range d.ignored() {
			rows = append(rows, d.row(ip, i == d.cursor[tabIgnored]))
		}
		if len(rows) == 0 {
			rows = append(rows, th.Subtitle().Render("Nothing ignored. Press n to add a path."))
		}
	}
	body := lipgloss.JoinVertical(lipgloss.Left, header, "", strings.Join(tabs, " "), "", strings.Join(rows, "\n"))
	if d.overlays.Active() {
		return components.OverlayOn(body, d.overlays.View(overlayWidth(d.ctx)), d.ctx.Width, d.ctx.Height)
	}
	return body
}

func (d *projectDetail) row(line string, selected bool) string {
	if selected {
		return d.ctx.Theme.Selected().Render(line)
	}
	return line
}

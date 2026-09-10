// Package dashboard is the Tasks / Inbox / History screen: stat tiles, filter
// chips and a table or kanban of tickets folded with their sessions. One model
// serves all three sections; Inbox and History pin a status filter. It also
// hosts the new-task form, the spawn wizard and the task detail drill-in.
package dashboard

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/adapter/in/tui/screens/spawn"
	"operators-mcp/internal/domain"
)

// Model is the dashboard state.
type Model struct {
	ctx    core.Context
	be     backend.Backend
	pinned rollup.Filter // FilterAll means the user may cycle the filter
	filter rollup.Filter
	// projectIdx indexes snap.Projects; -1 means every project.
	projectIdx int
	kanban     bool
	table      components.Table
	snap       backend.Snapshot
	rows       []rollup.Row

	overlays components.Overlays
	wizard   *spawn.Model
	detail   *taskDetail
}

// New builds a dashboard. Pass rollup.FilterAll for the Tasks section, or a
// pinned filter for Inbox (FilterAttention) and History (FilterDone).
func New(ctx core.Context, pinned rollup.Filter, be backend.Backend) Model {
	return Model{
		ctx:        ctx,
		be:         be,
		pinned:     pinned,
		filter:     pinned,
		projectIdx: -1,
		table: components.NewTable([]components.Column{
			{Title: "Task"}, {Title: "Project", Width: 14}, {Title: "Agents", Width: 7},
			{Title: "Status", Width: 16}, {Title: "Spend", Width: 8},
		}),
	}
}

// Filter is the active status filter.
func (m Model) Filter() rollup.Filter { return m.filter }

// Selected is the ticket under the cursor, or nil.
func (m Model) Selected() *domain.Ticket {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.rows) {
		return nil
	}
	return m.rows[i].Ticket
}

// Capturing reports whether an overlay or drill-in owns the keyboard.
func (m Model) Capturing() bool {
	return m.detail != nil || m.wizard != nil || m.overlays.Active()
}

// Hints lists the keys this screen answers to.
func (m Model) Hints() []core.KeyHint {
	if m.wizard != nil {
		return m.wizard.Hints()
	}
	if m.detail != nil {
		return m.detail.hints()
	}
	h := []core.KeyHint{{Key: "↵", Desc: "open task"}, {Key: "n", Desc: "new task"}, {Key: "s", Desc: "spawn agent"}, {Key: "j/k", Desc: "move"}, {Key: "p", Desc: "project"}, {Key: "v", Desc: "table/kanban"}}
	if m.pinned == rollup.FilterAll {
		h = append(h, core.KeyHint{Key: "f", Desc: "status filter"})
	}
	return h
}

// Summary is the subtitle the chrome shows next to the section title.
func (m Model) Summary() string {
	s := rollup.Stats(m.rows, m.snap.Sessions)
	switch m.pinned {
	case rollup.FilterAttention:
		return fmt.Sprintf("%d need attention", len(m.rows))
	case rollup.FilterDone:
		return fmt.Sprintf("%d completed", len(m.rows))
	}
	return fmt.Sprintf("%d tasks · %d agents running", len(m.snap.Tickets), s.AgentsWorking)
}

// Update routes a message through the layers of the screen, outermost first:
// context and snapshot reach everyone; the wizard, then the detail, own
// everything else while open; then the overlay; then the list itself.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		m.ctx = msg.Ctx
		return m.fanout(msg)
	case core.SnapshotMsg:
		m.snap = msg.Snapshot
		if m.projectIdx >= len(m.snap.Projects) {
			m.projectIdx = -1
		}
		m.rebuild()
		return m.fanout(msg)
	case closeDetailMsg:
		m.detail = nil
		return m, nil
	case spawn.DoneMsg:
		m.closeWizard()
		id := msg.Session.ID
		return m, func() tea.Msg { return core.NavigateMsg{SessionID: id} }
	case spawn.CancelMsg:
		m.closeWizard()
		return m, nil
	}
	if m.wizard != nil {
		w, cmd := m.wizard.Update(msg)
		m.wizard = &w
		return m, cmd
	}
	if m.detail != nil {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.update(msg)
		return m, cmd
	}
	if cmd, handled := m.overlays.Update(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		cmd, _ := m.overlays.Done(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

// closeWizard drops the spawn wizard wherever it was opened from.
func (m *Model) closeWizard() {
	m.wizard = nil
	if m.detail != nil {
		m.detail.wizard = nil
	}
}

// fanout forwards context and snapshot messages to the wizard and detail.
func (m Model) fanout(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.wizard != nil {
		w, cmd := m.wizard.Update(msg)
		m.wizard = &w
		cmds = append(cmds, cmd)
	}
	if m.detail != nil {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) key(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "j", "down":
		m.table.Move(1)
	case "k", "up":
		m.table.Move(-1)
	case "g", "home":
		m.table.First()
	case "G", "end":
		m.table.Last()
	case "f":
		if m.pinned == rollup.FilterAll {
			m.filter = nextFilter(m.filter)
			m.rebuild()
		}
	case "p":
		m.projectIdx++
		if m.projectIdx >= len(m.snap.Projects) {
			m.projectIdx = -1
		}
		m.rebuild()
	case "v":
		m.kanban = !m.kanban
	case "n":
		m.overlays.OpenForm(newTaskForm(m.ctx, m.snap, m.projectID()), createTicket(m.be))
	case "s":
		opts := spawn.Options{ProjectID: m.projectID()}
		if t := m.Selected(); t != nil {
			opts = spawn.Options{ProjectID: t.ProjectID, TicketID: t.ID}
		}
		w := spawn.New(m.ctx, m.be, m.snap, opts)
		m.wizard = &w
	case "enter":
		if t := m.Selected(); t != nil {
			m.detail = newTaskDetail(m.ctx, m.be, m.snap, t.ID)
		}
	}
	return m, nil
}

func newTaskForm(ctx core.Context, snap backend.Snapshot, projectID string) components.Form {
	var projects []components.Option
	for _, p := range snap.Projects {
		projects = append(projects, components.Option{Label: p.Name, Value: p.ID})
	}
	if projectID == "" && len(snap.Projects) > 0 {
		projectID = snap.Projects[0].ID
	}
	return components.NewForm(ctx.Theme, "New task",
		components.SelectOptions("project", "Project", projects, projectID),
		components.TextField("title", "Title", "").Required(),
		components.MultilineField("description", "Goal (markdown)", ""),
	)
}

// createTicket is the submit command of the new-task form.
func createTicket(be backend.Backend) components.SubmitFunc {
	return func(sub components.FormSubmitMsg) tea.Cmd {
		v := sub.Values
		return components.Op("task.create", "Task created", func() error {
			_, err := be.CreateTicket(v["project"], v["title"], v["description"], domain.TicketStatusTodo)
			return err
		})
	}
}

// nextFilter cycles All → In flight → Needs review → Blocked → Done → All.
func nextFilter(f rollup.Filter) rollup.Filter {
	switch f {
	case rollup.FilterAll:
		return rollup.FilterInFlight
	case rollup.FilterInFlight:
		return rollup.FilterReview
	case rollup.FilterReview:
		return rollup.FilterBlocked
	case rollup.FilterBlocked:
		return rollup.FilterDone
	}
	return rollup.FilterAll
}

func (m Model) projectID() string {
	if m.projectIdx < 0 || m.projectIdx >= len(m.snap.Projects) {
		return ""
	}
	return m.snap.Projects[m.projectIdx].ID
}

func (m *Model) rebuild() {
	m.rows = rollup.Join(m.snap.Tickets, m.snap.Sessions, m.projectID(), m.filter)
	rows := make([][]string, 0, len(m.rows))
	for _, r := range m.rows {
		rows = append(rows, []string{
			r.Ticket.Title,
			m.snap.ProjectName(r.Ticket.ProjectID),
			fmt.Sprintf("%d", len(r.Rollup.Sessions)),
			m.ctx.Theme.TaskPill(r.Rollup.Status),
			fmt.Sprintf("$%.2f", r.Rollup.CostUSD),
		})
	}
	m.table.SetRows(rows)
}

// View draws the detail, or tiles, chips and the table or kanban, with any
// overlay placed over the body.
func (m Model) View() string {
	th := m.ctx.Theme
	if m.detail != nil {
		return m.detail.view()
	}
	tiles := m.tiles()
	chips := m.chips()
	used := lipgloss.Height(tiles) + lipgloss.Height(chips) + 1
	bodyH := max(m.ctx.Height-used, 3)
	var body string
	switch {
	case len(m.rows) == 0:
		body = th.Subtitle().Render(m.emptyText())
	case m.kanban:
		body = m.kanbanView(bodyH)
	default:
		body = m.table.View(th, m.ctx.Width, bodyH)
	}
	out := lipgloss.JoinVertical(lipgloss.Left, tiles, chips, "", body)
	switch {
	case m.wizard != nil:
		return components.OverlayOn(out, m.wizard.View(), m.ctx.Width, m.ctx.Height)
	case m.overlays.Active():
		return components.OverlayOn(out, m.overlays.View(overlayWidth(m.ctx)), m.ctx.Width, m.ctx.Height)
	}
	return out
}

// overlayWidth fits a form or confirm to the body, capped for readability.
func overlayWidth(ctx core.Context) int { return min(ctx.Width-4, 80) }

func (m Model) emptyText() string {
	switch m.pinned {
	case rollup.FilterAttention:
		return "Nothing needs your attention."
	case rollup.FilterDone:
		return "No completed tasks yet."
	}
	if len(m.snap.Tickets) == 0 {
		return "No tasks yet. Press n to create one."
	}
	return "No tasks match this filter."
}

func (m Model) tiles() string {
	th := m.ctx.Theme
	s := rollup.Stats(rollup.Join(m.snap.Tickets, m.snap.Sessions, m.projectID(), rollup.FilterAll), m.snap.Sessions)
	return components.StatTiles(th, []components.Tile{
		{Label: "In flight", Value: fmt.Sprintf("%d", s.TasksInFlight), Color: th.StatusColor(rollup.Run)},
		{Label: "Agents working", Value: fmt.Sprintf("%d", s.AgentsWorking), Color: th.StatusColor(rollup.Think)},
		{Label: "Needs review", Value: fmt.Sprintf("%d", s.NeedsReview), Color: th.StatusColor(rollup.Review)},
		{Label: "Blocked", Value: fmt.Sprintf("%d", s.Blocked), Color: th.StatusColor(rollup.Block)},
		{Label: "Spend", Value: fmt.Sprintf("$%.2f", s.SpendUSD)},
	}, m.ctx.Width)
}

func (m Model) chips() string {
	th := m.ctx.Theme
	var parts []string
	if m.pinned == rollup.FilterAll {
		for _, f := range []rollup.Filter{rollup.FilterAll, rollup.FilterInFlight, rollup.FilterReview, rollup.FilterBlocked, rollup.FilterDone} {
			label := f.Label()
			if f == m.filter {
				parts = append(parts, th.AccentText().Render("["+label+"]"))
			} else {
				parts = append(parts, th.Subtitle().Render(" "+label+" "))
			}
		}
	}
	project := "all projects"
	if id := m.projectID(); id != "" {
		project = m.snap.ProjectName(id)
	}
	parts = append(parts, th.Meta().Render("project: ")+th.Title().Render(project))
	return strings.Join(parts, " ")
}

// kanbanView lays the rows out in four columns by rollup column.
func (m Model) kanbanView(height int) string {
	th := m.ctx.Theme
	cols := []rollup.Column{rollup.InFlight, rollup.ReviewColumn, rollup.BlockedColumn, rollup.DoneColumn}
	byCol := map[rollup.Column][]rollup.Row{}
	for _, r := range m.rows {
		c := rollup.ColumnFor(r.Rollup.Status)
		byCol[c] = append(byCol[c], r)
	}
	colW := max(m.ctx.Width/len(cols)-2, 12)
	selected := m.Selected()
	var rendered []string
	for _, c := range cols {
		lines := []string{th.Meta().Bold(true).Render(fmt.Sprintf("%s (%d)", c.Label(), len(byCol[c])))}
		for _, r := range byCol[c] {
			card := r.Ticket.Title
			meta := fmt.Sprintf("%d agents · $%.2f", len(r.Rollup.Sessions), r.Rollup.CostUSD)
			if selected != nil && selected.ID == r.Ticket.ID {
				card = th.Selected().Render(card)
			}
			lines = append(lines, card, th.Meta().Render(meta))
		}
		rendered = append(rendered, th.Panel().Width(colW).Height(height-2).Render(strings.Join(lines, "\n")))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

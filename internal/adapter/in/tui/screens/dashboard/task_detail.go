package dashboard

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/adapter/in/tui/screens/spawn"
	"operators-mcp/internal/domain"
)

// closeDetailMsg asks the dashboard to drop the drill-in.
type closeDetailMsg struct{}

func closeDetail() tea.Msg { return closeDetailMsg{} }

const (
	focusDocs = iota
	focusAgents
)

// taskDetail is one task: markdown hero, stats, its documents and its agents.
type taskDetail struct {
	ctx         core.Context
	be          backend.Backend
	snap        backend.Snapshot
	ticketID    string
	docs        []*domain.Document
	focus       int
	cursor      [2]int
	reader      *viewport.Model
	readerTitle string
	wizard      *spawn.Model
	overlays    components.Overlays
}

func newTaskDetail(ctx core.Context, be backend.Backend, snap backend.Snapshot, ticketID string) *taskDetail {
	d := &taskDetail{ctx: ctx, be: be, snap: snap, ticketID: ticketID}
	d.reload()
	return d
}

func (d *taskDetail) reload() { d.docs = d.be.ListTicketDocuments(d.ticketID) }

func (d *taskDetail) ticket() *domain.Ticket {
	for _, t := range d.snap.Tickets {
		if t.ID == d.ticketID {
			return t
		}
	}
	return nil
}

func (d *taskDetail) sessions() []*domain.Session {
	return rollup.ByTicket(d.snap.Sessions)[d.ticketID]
}

func (d *taskDetail) clamp() {
	counts := [2]int{len(d.docs), len(d.sessions())}
	for i := range d.cursor {
		if d.cursor[i] >= counts[i] {
			d.cursor[i] = counts[i] - 1
		}
		if d.cursor[i] < 0 {
			d.cursor[i] = 0
		}
	}
}

func (d *taskDetail) hints() []core.KeyHint {
	if d.wizard != nil {
		return d.wizard.Hints()
	}
	if d.reader != nil {
		return []core.KeyHint{{Key: "esc", Desc: "close"}, {Key: "j/k", Desc: "scroll"}}
	}
	return []core.KeyHint{{Key: "esc", Desc: "back"}, {Key: "tab", Desc: "docs/agents"}, {Key: "↵", Desc: "open"}, {Key: "s", Desc: "spawn"}, {Key: "e", Desc: "edit"}, {Key: "x", Desc: "status"}, {Key: "D", Desc: "delete"}}
}

func (d *taskDetail) update(msg tea.Msg) (*taskDetail, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		d.ctx = msg.Ctx
		return d, d.forwardToWizard(msg)
	case core.SnapshotMsg:
		d.snap = msg.Snapshot
		if d.ticket() == nil {
			return d, closeDetail
		}
		d.reload()
		d.clamp()
		return d, d.forwardToWizard(msg)
	}
	if d.wizard != nil {
		return d, d.forwardToWizard(msg)
	}
	if cmd, handled := d.overlays.Update(msg); handled {
		return d, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		cmd, _ := d.overlays.Done(msg)
		return d, cmd
	case tea.KeyPressMsg:
		if d.reader != nil {
			return d, d.readerKey(msg)
		}
		return d, d.key(msg)
	}
	return d, nil
}

func (d *taskDetail) forwardToWizard(msg tea.Msg) tea.Cmd {
	if d.wizard == nil {
		return nil
	}
	w, cmd := d.wizard.Update(msg)
	d.wizard = &w
	return cmd
}

func (d *taskDetail) readerKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "q":
		d.reader = nil
		return nil
	}
	vp, cmd := d.reader.Update(k)
	d.reader = &vp
	return cmd
}

func (d *taskDetail) key(k tea.KeyPressMsg) tea.Cmd {
	th, be := d.ctx.Theme, d.be
	t := d.ticket()
	if t == nil {
		return closeDetail
	}
	id := t.ID
	switch k.String() {
	case "esc", "q":
		return closeDetail
	case "tab", "h", "l", "left", "right":
		d.focus = (d.focus + 1) % 2
	case "j", "down":
		d.cursor[d.focus]++
		d.clamp()
	case "k", "up":
		d.cursor[d.focus]--
		d.clamp()
	case "enter":
		if d.focus == focusDocs {
			if i := d.cursor[focusDocs]; i < len(d.docs) {
				d.openReader(d.docs[i])
			}
			return nil
		}
		ss := d.sessions()
		if i := d.cursor[focusAgents]; i < len(ss) {
			sessionID := ss[i].ID
			return func() tea.Msg { return core.NavigateMsg{SessionID: sessionID} }
		}
	case "s":
		w := spawn.New(d.ctx, d.be, d.snap, spawn.Options{ProjectID: t.ProjectID, TicketID: t.ID})
		d.wizard = &w
	case "e":
		status := t.Status
		d.overlays.OpenForm(components.NewForm(th, "Edit task",
			components.TextField("title", "Title", t.Title).Required(),
			components.MultilineField("description", "Goal (markdown)", t.Description),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			v := sub.Values
			return components.Op("task.update", "Task updated", func() error {
				_, err := be.UpdateTicket(id, v["title"], v["description"], status)
				return err
			})
		})
	case "x":
		statuses := []string{string(domain.TicketStatusBacklog), string(domain.TicketStatusTodo), string(domain.TicketStatusInProgress), string(domain.TicketStatusReview), string(domain.TicketStatusDone)}
		title, desc := t.Title, t.Description
		d.overlays.OpenForm(components.NewForm(th, "Task status",
			components.SelectField("status", "Status", statuses, string(t.Status)),
		), func(sub components.FormSubmitMsg) tea.Cmd {
			status := domain.TicketStatus(sub.Values["status"])
			return components.Op("task.status", "Status changed", func() error {
				_, err := be.UpdateTicket(id, title, desc, status)
				return err
			})
		})
	case "D":
		d.overlays.OpenConfirm(th, "Delete task",
			fmt.Sprintf("Delete %q? Its sessions keep their history but lose the link.", t.Title),
			func() tea.Cmd {
				return components.Op("task.delete", "Task deleted", func() error { return be.DeleteTicket(id) })
			})
	}
	return nil
}

func (d *taskDetail) openReader(doc *domain.Document) {
	w := d.ctx.Width - 4
	h := d.ctx.Height - 4
	vp := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
	vp.SetContent(d.ctx.Theme.Markdown(doc.Content, w-2))
	d.reader = &vp
	d.readerTitle = doc.Title
}

func (d *taskDetail) view() string {
	th := d.ctx.Theme
	t := d.ticket()
	if t == nil {
		return ""
	}
	r := rollup.Fold(d.sessions())
	var lines []string
	lines = append(lines, th.Title().Render(t.Title)+"  "+th.TaskPill(r.Status)+"  "+th.Meta().Render(d.snap.ProjectName(t.ProjectID)+" · "+string(t.Status)))
	if strings.TrimSpace(t.Description) != "" {
		lines = append(lines, th.Markdown(t.Description, min(d.ctx.Width-4, 100)))
	}
	lines = append(lines, th.Meta().Render(fmt.Sprintf("%d agents · %d running · %d needs review · %d blocked · $%.2f · %d docs",
		len(r.Sessions), r.RunningCount, r.ReviewCount, r.BlockedCount, r.CostUSD, len(d.docs))), "")

	lines = append(lines, d.sectionTitle("Documents on this task", d.focus == focusDocs))
	if len(d.docs) == 0 {
		lines = append(lines, th.Subtitle().Render("  none linked"))
	}
	for i, doc := range d.docs {
		lines = append(lines, d.row("  "+doc.Title, d.focus == focusDocs && i == d.cursor[focusDocs]))
	}
	lines = append(lines, "", d.sectionTitle("Agents on this task", d.focus == focusAgents))
	ss := d.sessions()
	if len(ss) == 0 {
		lines = append(lines, th.Subtitle().Render("  none yet · press s to spawn one"))
	}
	for i, s := range ss {
		line := fmt.Sprintf("  %-28s %s  %s", firstLine(s.Task, 28), th.StatusPill(rollup.AgentStatusOf(s.Status)), th.Meta().Render(fmt.Sprintf("%s · $%.2f", s.Branch, s.CostUSD)))
		lines = append(lines, d.row(line, d.focus == focusAgents && i == d.cursor[focusAgents]))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	switch {
	case d.reader != nil:
		panel := th.PanelFocused().Render(th.Title().Render(d.readerTitle) + "\n\n" + d.reader.View())
		return components.OverlayOn(body, panel, d.ctx.Width, d.ctx.Height)
	case d.wizard != nil:
		return components.OverlayOn(body, d.wizard.View(), d.ctx.Width, d.ctx.Height)
	case d.overlays.Active():
		return components.OverlayOn(body, d.overlays.View(overlayWidth(d.ctx)), d.ctx.Width, d.ctx.Height)
	}
	return body
}

func (d *taskDetail) sectionTitle(s string, focused bool) string {
	if focused {
		return d.ctx.Theme.AccentText().Render("▸ " + s)
	}
	return d.ctx.Theme.Subtitle().Render("  " + s)
}

func (d *taskDetail) row(line string, selected bool) string {
	if selected {
		return d.ctx.Theme.Selected().Render(line)
	}
	return line
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

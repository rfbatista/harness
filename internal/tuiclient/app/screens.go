package app

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/tuiclient/term"
)

// moveCursor applies the list navigation keys; ok is false for other keys.
func moveCursor(k string, cursor, n int) (int, bool) {
	switch k {
	case "up", "k":
		return clamp(cursor-1, n), true
	case "down", "j":
		return clamp(cursor+1, n), true
	case "home", "g":
		return 0, true
	case "end", "G":
		return clamp(n-1, n), true
	}
	return cursor, false
}

// --- projects ---------------------------------------------------------------

func (m Model) projectsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if c, ok := moveCursor(k, m.projCursor, len(m.projects)); ok {
		m.projCursor = c
		return m, nil
	}
	switch k {
	case "q":
		return m, tea.Quit
	case "r":
		m.loading, m.status = true, ""
		return m, m.loadProjects()
	case "enter", "right", "l":
		if len(m.projects) == 0 {
			return m, nil
		}
		p := m.projects[m.projCursor]
		if p.ID != m.project.ID {
			m.tickets, m.sessions, m.taskCursor = nil, nil, 0
		}
		m.project, m.screen = p, screenTasks
		m.loading, m.status = true, ""
		return m, m.loadTickets(p.ID)
	}
	return m, nil
}

func (m Model) projectsView(height int) string {
	if len(m.projects) == 0 {
		if m.loading {
			return ""
		}
		return noteStyle.Render("  No projects yet. Create one in coding_pool first. (r to refresh, q to quit)")
	}
	rows := make([]string, 0, height)
	from, to := window(m.projCursor, len(m.projects), height-1)
	nameW := min(32, max(12, m.width/3))
	for i := from; i < to; i++ {
		p := m.projects[i]
		live := ""
		if n := m.liveInProject(p.ID); n > 0 {
			live = liveStyle.Render(fmt.Sprintf("● %d live", n))
		}
		row := " " + fit(p.Name, nameW) + "  " + fit(dimStyle.Render(p.RootDir), max(0, m.width-nameW-14)) + " " + live
		rows = append(rows, m.cursorRow(row, i == m.projCursor))
	}
	rows = append(rows, noteStyle.Render(" enter open · r refresh · q quit"))
	return strings.Join(rows, "\n")
}

func (m Model) cursorRow(row string, selected bool) string {
	if selected {
		return cursorStyle.Render(fit(row, m.width))
	}
	return row
}

// --- tasks --------------------------------------------------------------------

// statusOrder is the kanban order tasks are listed in: work in flight first.
var statusOrder = []domain.TicketStatus{
	domain.TicketStatusInProgress, domain.TicketStatusReview, domain.TicketStatusTodo,
	domain.TicketStatusBacklog, domain.TicketStatusDone,
}

func sortTickets(ts []domain.Ticket) []domain.Ticket {
	rank := func(s domain.TicketStatus) int {
		if i := slices.Index(statusOrder, s); i >= 0 {
			return i
		}
		return len(statusOrder)
	}
	slices.SortStableFunc(ts, func(a, b domain.Ticket) int { return rank(a.Status) - rank(b.Status) })
	return ts
}

func statusLabel(s domain.TicketStatus) string {
	return strings.ReplaceAll(string(s), "_", " ")
}

func (m Model) tasksKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if c, ok := moveCursor(k, m.taskCursor, len(m.tickets)); ok {
		m.taskCursor = c
		return m, nil
	}
	switch k {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "left", "h":
		return m.back(), nil
	case "r":
		m.loading, m.status = true, ""
		return m, m.loadTickets(m.project.ID)
	case "enter", "right", "l":
		if len(m.tickets) == 0 {
			return m, nil
		}
		m.ticket, m.screen, m.status = m.tickets[m.taskCursor], screenTask, ""
		m.deck() // so the task opens with its tab bar sized
		return m, m.loadSessions(m.project.ID)
	}
	return m, nil
}

func (m Model) sessionCount(ticketID string) int {
	n := 0
	for _, s := range m.sessions {
		if s.TicketID == ticketID {
			n++
		}
	}
	return n
}

func (m Model) tasksView(height int) string {
	if len(m.tickets) == 0 {
		if m.loading {
			return ""
		}
		return noteStyle.Render("  This project has no tasks. (esc back, r refresh)")
	}
	rows := make([]string, 0, height)
	from, to := window(m.taskCursor, len(m.tickets), height-1)
	titleW := max(12, m.width-40)
	for i := from; i < to; i++ {
		t := m.tickets[i]
		sessions := ""
		if n := m.sessionCount(t.ID); n > 0 {
			sessions = dimStyle.Render(fmt.Sprintf("%d session%s", n, plural(n)))
		}
		live := ""
		if n := m.liveIn(t.ID); n > 0 {
			live = liveStyle.Render(fmt.Sprintf("● %d live", n))
		}
		row := " " + fit(dimStyle.Render(statusLabel(t.Status)), 12) + " " + fit(t.Title, titleW) + " " + fit(sessions, 12) + " " + live
		rows = append(rows, m.cursorRow(row, i == m.taskCursor))
	}
	rows = append(rows, noteStyle.Render(" enter open · esc back · r refresh · q quit"))
	return strings.Join(rows, "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// --- task ---------------------------------------------------------------------

// taskKey: with panes open, keys belong to the deck (and so to claude); the
// deck's own commands reach the client as messages. With none, the task
// screen takes plain keys.
func (m Model) taskKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.deck()
	if d.Len() > 0 || d.Awaiting() {
		return m.toDeck(msg)
	}
	switch msg.String() {
	case "n", "c", "enter":
		return m.openPicker()
	case "s":
		return m.Update(showSessionsMsg{})
	case "esc", "backspace", "b", "left", "h":
		return m.back(), nil
	case "q":
		return m, tea.Quit
	}
	return m.toDeck(msg) // the prefix key
}

func (m Model) taskView(height int) (string, *tea.Cursor) {
	d := m.deck()
	if d.Len() > 0 {
		return d.View(), d.Cursor()
	}
	lines := []string{
		d.View(),
		"",
		"  " + m.ticket.Title,
		"  " + dimStyle.Render(statusLabel(m.ticket.Status)),
	}
	if m.ticket.Description != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(m.ticket.Description, "\n") {
			lines = append(lines, "  "+dimStyle.Render(l))
		}
	}
	lines = append(lines, "",
		noteStyle.Render("  No agent sessions open. n start one · s past sessions · esc back · q quit"))
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n"), nil
}

// --- launching panes ----------------------------------------------------------

// launchedMsg carries a session the server started or resumed, with how to
// run its CLI.
type launchedMsg struct {
	ticketID string
	label    string
	session  domain.Session
	launch   launchSpec
	err      error
}

type launchSpec struct {
	dir  string
	args []string
	env  []string
}

// launched runs a started session's claude in a new pane of its task's deck.
// If claude cannot start, the session is ended so it does not linger as
// running.
func (m Model) launched(msg launchedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.err != nil {
		return m.fail(msg.err), nil
	}
	d := m.decksFor(msg.ticketID)
	w, h := d.PaneSize()
	pane := term.New(term.Options{
		Command: m.claude,
		Args:    msg.launch.args,
		Env:     msg.launch.env,
		Dir:     msg.launch.dir,
		Name:    msg.label,
		Width:   w,
		Height:  h,
	})
	if err := pane.Start(); err != nil {
		m = m.fail(err)
		return m, m.end(msg.session.ID, 127, false)
	}
	m.panes[pane.ID()] = paneRef{ticketID: msg.ticketID, sessionID: msg.session.ID}
	d, cmd := d.Add(pane)
	m.decks[msg.ticketID] = d
	m = m.note("started " + msg.session.Branch)
	return m, tea.Batch(cmd, m.loadSessions(m.project.ID))
}

// decksFor is the deck of any task, created on first use.
func (m *Model) decksFor(ticketID string) term.Deck {
	if ticketID == m.ticket.ID {
		return m.deck()
	}
	if d, ok := m.decks[ticketID]; ok {
		return d
	}
	saved := m.ticket
	m.ticket = domain.Ticket{ID: ticketID}
	d := m.deck()
	m.ticket = saved
	return d
}

// paneOf returns the open pane running sessionID.
func (m Model) paneOf(sessionID string) (int64, string, bool) {
	for id, ref := range m.panes {
		if ref.sessionID == sessionID {
			return id, ref.ticketID, true
		}
	}
	return 0, "", false
}

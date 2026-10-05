// Package tasks is a project's tasks in kanban order, each with how many
// sessions it has had and how many agents run in it now.
package tasks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/live"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/panes"
	"operators-mcp/internal/tuiclient/ui"
)

// Screen lists one project's tasks; enter opens one with Open.
type Screen struct {
	board    ports.TicketBoard
	sessions ports.SessionReader
	panes    *panes.Registry
	open     func(*domain.Project, *domain.Ticket) nav.Screen

	project  *domain.Project
	tickets  []*domain.Ticket
	recorded []*domain.Session
	cursor   int
	loading  bool
}

// New returns the tasks screen of project. open builds the screen a chosen
// task opens into.
func New(board ports.TicketBoard, sessions ports.SessionReader, reg *panes.Registry, project *domain.Project, open func(*domain.Project, *domain.Ticket) nav.Screen) *Screen {
	return &Screen{board: board, sessions: sessions, panes: reg, project: project, open: open, loading: true}
}

type (
	loadedMsg struct {
		projectID string
		tickets   []*domain.Ticket
		sessions  []*domain.Session
		err       error
	}
	sessionsMsg struct {
		projectID string
		sessions  []*domain.Session
	}
)

func (s *Screen) load() tea.Cmd {
	board, sessions, pid := s.board, s.sessions, s.project.ID
	return nav.Call(func(ctx context.Context) tea.Msg {
		ts, err := board.ListTickets(ctx, pid)
		if err != nil {
			return loadedMsg{projectID: pid, err: err}
		}
		ss, err := sessions.List(ctx, ports.SessionFilter{ProjectID: pid})
		return loadedMsg{projectID: pid, tickets: ts, sessions: ss, err: err}
	})
}

func (s *Screen) reloadSessions() tea.Cmd {
	sessions, pid := s.sessions, s.project.ID
	return nav.Call(func(ctx context.Context) tea.Msg {
		ss, err := sessions.List(ctx, ports.SessionFilter{ProjectID: pid})
		if err != nil {
			return nil // the counts stay as they were; the next load retries
		}
		return sessionsMsg{projectID: pid, sessions: ss}
	})
}

func (s *Screen) Init() tea.Cmd { return s.load() }

func (s *Screen) Crumb() string { return s.project.Name }

func (s *Screen) ProjectID() string { return s.project.ID }

func (s *Screen) Capturing() bool { return false }

func (s *Screen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		if msg.projectID != s.project.ID {
			return s, nil
		}
		s.loading = false
		if msg.err != nil {
			return s, nav.Fail(msg.err)
		}
		s.tickets = sortTickets(msg.tickets)
		s.recorded = msg.sessions
		s.cursor = ui.Clamp(s.cursor, len(s.tickets))
	case sessionsMsg:
		if msg.projectID == s.project.ID {
			s.recorded = msg.sessions
		}
	case panes.ChangedMsg:
		if msg.ProjectID == s.project.ID {
			return s, s.reloadSessions()
		}
	case live.ChangeMsg:
		if msg.ProjectID == s.project.ID {
			s.recorded = live.Apply(s.recorded, msg.Change, func(*domain.Session) bool { return true })
		}
	case live.ResyncMsg:
		if msg.ProjectID == s.project.ID {
			return s, s.load()
		}
	case tea.KeyPressMsg:
		return s.key(msg)
	}
	return s, nil
}

func (s *Screen) key(msg tea.KeyPressMsg) (nav.Screen, tea.Cmd) {
	k := msg.String()
	if c, ok := ui.MoveCursor(k, s.cursor, len(s.tickets)); ok {
		s.cursor = c
		return s, nil
	}
	switch k {
	case "q":
		return s, tea.Quit
	case "esc", "backspace", "left", "h":
		return s, nav.Pop()
	case "r":
		s.loading = true
		return s, s.load()
	case "enter", "right", "l":
		if len(s.tickets) == 0 {
			return s, nil
		}
		return s, nav.Push(s.open(s.project, s.tickets[s.cursor]))
	}
	return s, nil
}

// statusOrder is the kanban order tasks are listed in: work in flight first.
var statusOrder = []domain.TicketStatus{
	domain.TicketStatusInProgress, domain.TicketStatusReview, domain.TicketStatusTodo,
	domain.TicketStatusBacklog, domain.TicketStatusDone,
}

func sortTickets(ts []*domain.Ticket) []*domain.Ticket {
	rank := func(s domain.TicketStatus) int {
		if i := slices.Index(statusOrder, s); i >= 0 {
			return i
		}
		return len(statusOrder)
	}
	slices.SortStableFunc(ts, func(a, b *domain.Ticket) int { return rank(a.Status) - rank(b.Status) })
	return ts
}

func (s *Screen) sessionCount(ticketID string) int {
	n := 0
	for _, x := range s.recorded {
		if x.TicketID == ticketID {
			n++
		}
	}
	return n
}

func (s *Screen) View(width, height int) (string, *tea.Cursor) {
	if len(s.tickets) == 0 {
		if s.loading {
			return ui.Note.Render("  loading…"), nil
		}
		return ui.Note.Render("  This project has no tasks. (esc back, r refresh)"), nil
	}
	rows := make([]string, 0, height)
	from, to := ui.Window(s.cursor, len(s.tickets), height-1)
	titleW := max(12, width-40)
	for i := from; i < to; i++ {
		t := s.tickets[i]
		sessions := ""
		if n := s.sessionCount(t.ID); n > 0 {
			sessions = ui.Dim.Render(fmt.Sprintf("%d session%s", n, ui.Plural(n)))
		}
		live := ""
		if n := s.panes.LiveIn(t.ID); n > 0 {
			live = ui.Live.Render(fmt.Sprintf("● %d live", n))
		}
		row := " " + ui.Fit(ui.Dim.Render(ui.StatusLabel(t.Status)), 12) + " " + ui.Fit(t.Title, titleW) + " " + ui.Fit(sessions, 12) + " " + live
		rows = append(rows, ui.Row(row, i == s.cursor, width))
	}
	rows = append(rows, ui.Note.Render(" enter open · esc back · r refresh · q quit"))
	return strings.Join(rows, "\n"), nil
}

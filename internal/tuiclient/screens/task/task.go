// Package task is one task's screen: its deck of agent panes, the picker that
// starts a new session, and the overlay listing the sessions it has had.
package task

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/panes"
	"operators-mcp/internal/tuiclient/ui"
)

// Ports are the driving ports the task screen uses.
type Ports struct {
	Agents       ports.AgentCatalog
	Repositories ports.RepositoryCatalog
	Sessions     ports.SessionReader
	Interactive  ports.InteractiveSessions
}

// Screen is one task. With panes open every key goes to the focused claude,
// and the deck's prefix commands come back as panes messages.
type Screen struct {
	ports   Ports
	panes   *panes.Registry
	project *domain.Project
	ticket  *domain.Ticket

	recorded []*domain.Session // this task's sessions, for the overlay
	overlay  overlay
	width    int
}

// overlay is a modal over the task; at most one is open.
type overlay interface {
	update(s *Screen, msg tea.Msg) tea.Cmd
	view(s *Screen, width, height int) string
}

// New returns the screen of ticket in project.
func New(p Ports, reg *panes.Registry, project *domain.Project, ticket *domain.Ticket) *Screen {
	return &Screen{ports: p, panes: reg, project: project, ticket: ticket, width: 80}
}

type sessionsMsg struct {
	ticketID string
	sessions []*domain.Session
	err      error
}

func (s *Screen) loadSessions() tea.Cmd {
	sessions, tid, pid := s.ports.Sessions, s.ticket.ID, s.project.ID
	return nav.Call(func(ctx context.Context) tea.Msg {
		ss, err := sessions.List(ctx, ports.SessionFilter{ProjectID: pid, TicketID: tid})
		return sessionsMsg{ticketID: tid, sessions: ss, err: err}
	})
}

func (s *Screen) Init() tea.Cmd {
	s.panes.Deck(s.ticket.ID) // so the task opens with its tab bar sized
	return s.loadSessions()
}

func (s *Screen) Crumb() string { return s.ticket.Title }

// Capturing is always true: with panes open, ctrl+c belongs to claude.
func (s *Screen) Capturing() bool { return true }

func (s *Screen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
	case sessionsMsg:
		if msg.ticketID == s.ticket.ID && msg.err == nil {
			s.recorded = msg.sessions
		}
		return s, nil
	case panes.ChangedMsg:
		if msg.ProjectID == s.project.ID {
			return s, s.loadSessions()
		}
		return s, nil
	case panes.NewSessionMsg:
		return s, s.openPicker()
	case panes.ShowSessionsMsg:
		s.overlay = &sessionsOverlay{}
		return s, s.loadSessions()
	case panes.BackMsg:
		s.overlay = nil
		return s, nav.Pop()
	case tea.KeyPressMsg:
		if s.overlay != nil {
			return s, s.overlay.update(s, msg)
		}
		return s.key(msg)
	case tea.PasteMsg:
		if s.overlay != nil {
			return s, s.overlay.update(s, msg)
		}
		return s, s.panes.Input(s.ticket.ID, msg)
	}
	if s.overlay != nil {
		return s, s.overlay.update(s, msg)
	}
	return s, nil
}

// key: with panes open, keys belong to the deck (and so to claude). With
// none, the task screen takes plain keys.
func (s *Screen) key(msg tea.KeyPressMsg) (nav.Screen, tea.Cmd) {
	d := s.panes.Deck(s.ticket.ID)
	if d.Len() > 0 || d.Awaiting() {
		return s, s.panes.Input(s.ticket.ID, msg)
	}
	switch msg.String() {
	case "n", "c", "enter":
		return s, s.openPicker()
	case "s":
		return s.Update(panes.ShowSessionsMsg{})
	case "esc", "backspace", "b", "left", "h":
		return s, nav.Pop()
	case "q":
		return s, tea.Quit
	}
	return s, s.panes.Input(s.ticket.ID, msg) // the prefix key
}

func (s *Screen) View(width, height int) (string, *tea.Cursor) {
	if s.overlay != nil {
		return s.overlay.view(s, width, height), nil
	}
	d := s.panes.Deck(s.ticket.ID)
	if d.Len() > 0 {
		return d.View(), d.Cursor()
	}
	lines := []string{
		d.View(),
		"",
		"  " + s.ticket.Title,
		"  " + ui.Dim.Render(ui.StatusLabel(s.ticket.Status)),
	}
	if s.ticket.Description != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(s.ticket.Description, "\n") {
			lines = append(lines, "  "+ui.Dim.Render(l))
		}
	}
	lines = append(lines, "",
		ui.Note.Render("  No agent sessions open. n start one · s past sessions · esc back · q quit"))
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n"), nil
}

// launch asks the server for a session with call and hands it to the pane
// registry, which opens its pane even if the user has left the task by then.
func (s *Screen) launch(label string, call func(ctx context.Context) (*domain.Session, ports.AgentSpec, error)) tea.Cmd {
	pid, tid := s.project.ID, s.ticket.ID
	return nav.Call(func(ctx context.Context) tea.Msg {
		sess, spec, err := call(ctx)
		return panes.LaunchMsg{ProjectID: pid, TicketID: tid, Label: label, Session: sess, Spec: spec, Err: err}
	})
}

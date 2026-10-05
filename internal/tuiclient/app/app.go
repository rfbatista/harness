// Package app is tui-client's root model: a stack of screens — projects, a
// project's tasks, one task — under a header, and the pane registry that
// keeps every task's agents running while their screen is not shown.
//
// It is a client of the coding_pool server through the driving ports in
// internal/ports; tui-client binds them to the HTTP adapters, so the server
// provisions, records and ends the sessions and the client runs their CLIs.
package app

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/live"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/panes"
	"operators-mcp/internal/tuiclient/screens/projects"
	"operators-mcp/internal/tuiclient/screens/task"
	"operators-mcp/internal/tuiclient/screens/tasks"
	"operators-mcp/internal/tuiclient/ui"
)

const headerHeight = 1

// Deps are the driving ports the screens use, and where agents run.
type Deps struct {
	Projects     ports.ProjectReader
	Repositories ports.RepositoryCatalog
	Board        ports.TicketBoard
	Agents       ports.AgentCatalog
	Sessions     ports.SessionReader
	Interactive  ports.InteractiveSessions
	// Terminals attaches to sessions the server runs.
	Terminals ports.TerminalAccess
	// Feed keeps the lists current; nil leaves them to be refreshed by hand.
	Feed ports.SessionFeed
	// RunsOn is where sessions this client starts run; Host runs them when
	// that is here (RunnerTUI), and RunnerHost names this machine on them.
	RunsOn     domain.Runner
	Host       ports.TerminalHost
	RunnerHost string
}

// Model is the root. Key presses go to the screen on top; pane events and
// launches go to the registry; everything else goes to every screen on the
// stack, so a list underneath stays current.
type Model struct {
	stack    []nav.Screen
	panes    *panes.Registry
	sessions ports.SessionReader
	live     *live.Follower // nil without a feed

	width, height int
	status        string
	isErr         bool
}

// New builds the root over d, opening on the projects screen.
func New(d Deps) Model {
	reg := panes.New(panes.Config{
		RunsOn:     d.RunsOn,
		RunnerHost: d.RunnerHost,
		Host:       d.Host,
		Terminals:  d.Terminals,
		Sessions:   d.Interactive,
	})
	openTask := func(p *domain.Project, t *domain.Ticket) nav.Screen {
		return task.New(task.Ports{
			Agents:       d.Agents,
			Repositories: d.Repositories,
			Sessions:     d.Sessions,
			Interactive:  d.Interactive,
		}, reg, p, t)
	}
	openProject := func(p *domain.Project) nav.Screen {
		return tasks.New(d.Board, d.Sessions, reg, p, openTask)
	}
	var follower *live.Follower
	if d.Feed != nil {
		follower = live.New(d.Feed)
	}
	return Model{
		stack:    []nav.Screen{projects.New(d.Projects, reg, openProject)},
		panes:    reg,
		sessions: d.Sessions,
		live:     follower,
		width:    80,
		height:   24,
	}
}

func (m Model) Init() tea.Cmd { return tea.Batch(m.top().Init(), m.reattach()) }

// reattach opens a pane on every session the server is running: they
// outlived the client that started them.
func (m Model) reattach() tea.Cmd {
	sessions := m.sessions
	return nav.Call(func(ctx context.Context) tea.Msg {
		running, err := sessions.List(ctx, ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionRunning}})
		if err != nil {
			return nav.Fail(err)()
		}
		var cmds []tea.Cmd
		for _, s := range running {
			if s.Interactive && s.RunsOn == domain.RunnerServer {
				cmds = append(cmds, func() tea.Msg { return panes.AttachMsg{Session: s} })
			}
		}
		if len(cmds) == 0 {
			return nil
		}
		return tea.BatchMsg(cmds)
	})
}

func (m Model) top() nav.Screen { return m.stack[len(m.stack)-1] }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, ok := m.panes.Update(msg); ok {
		return m, cmd
	}
	if m.live != nil {
		if cmd, ok := m.live.Update(msg); ok {
			return m, cmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.panes.Resize(msg.Width, m.bodyHeight())
		return m.broadcast(tea.WindowSizeMsg{Width: msg.Width, Height: m.bodyHeight()})

	case nav.PushMsg:
		m.stack = append(m.stack, msg.Screen)
		m.status = ""
		return m, tea.Batch(msg.Screen.Init(), m.follow())
	case nav.PopMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		m.status = ""
		return m, m.follow()
	case nav.StatusMsg:
		m.status, m.isErr = msg.Text, msg.Err
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" && !m.top().Capturing() {
			return m, tea.Quit
		}
		return m.toTop(msg)
	case tea.PasteMsg:
		return m.toTop(msg)
	}
	return m.broadcast(msg)
}

// follow points the live feed at the project of the deepest screen inside
// one, or at none.
func (m Model) follow() tea.Cmd {
	if m.live == nil {
		return nil
	}
	project := ""
	for _, s := range m.stack {
		if p, ok := s.(nav.ProjectScoped); ok {
			project = p.ProjectID()
		}
	}
	if project == m.live.Project() {
		return nil
	}
	return m.live.Follow(project)
}

func (m Model) toTop(msg tea.Msg) (tea.Model, tea.Cmd) {
	i := len(m.stack) - 1
	next, cmd := m.stack[i].Update(msg)
	m.stack[i] = next
	return m, cmd
}

func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(m.stack))
	for i, s := range m.stack {
		next, cmd := s.Update(msg)
		m.stack[i] = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) bodyHeight() int { return max(1, m.height-headerHeight) }

// Shutdown stops every pane and ends its session as closed. The host calls it
// after the program exits.
func (m Model) Shutdown(ctx context.Context) { m.panes.Shutdown(ctx) }

func (m Model) View() tea.View {
	body, cursor := m.top().View(m.width, m.bodyHeight())
	if cursor != nil {
		cursor.Y += headerHeight
	}
	v := tea.NewView(m.header() + "\n" + body)
	v.AltScreen = true
	v.Cursor = cursor
	return v
}

func (m Model) header() string {
	crumbs := []string{ui.Header.Render("coding pool")}
	for _, s := range m.stack {
		if c := s.Crumb(); c != "" {
			crumbs = append(crumbs, c)
		}
	}
	left := strings.Join(crumbs, ui.Crumb.Render(" › "))

	var right string
	switch {
	case m.status != "" && m.isErr:
		right = ui.Error.Render(m.status)
	case m.status != "":
		right = ui.Note.Render(m.status)
	case m.live != nil && m.live.Lost():
		right = ui.Error.Render("○ live updates paused, reconnecting…")
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return ansi.Truncate(left+" "+right, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

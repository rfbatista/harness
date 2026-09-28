package workbench

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"operators-mcp/internal/adapter/in/termpane"
	"operators-mcp/internal/domain"
)

const (
	headerHeight = 1
	callTimeout  = 2 * time.Minute // starting a session runs git worktree add
)

type screen int

const (
	screenProjects screen = iota
	screenTasks
	screenTask
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	crumbStyle  = lipgloss.NewStyle().Faint(true)
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	noteStyle   = lipgloss.NewStyle().Faint(true)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	liveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle    = lipgloss.NewStyle().Faint(true)
)

// Model is the workbench root. Screens form a stack — projects, a project's
// tasks, one task — and each task that has sessions open keeps its own deck of
// panes, so leaving a task never stops its agents.
type Model struct {
	be     Backend
	claude string // the claude binary panes run

	width, height int
	screen        screen

	projects   []domain.Project
	projCursor int
	project    domain.Project

	tickets    []domain.Ticket
	taskCursor int
	ticket     domain.Ticket
	// sessions are the current project's recorded sessions, for the counts in
	// the task list and the task's sessions overlay.
	sessions []domain.Session

	decks     map[string]termpane.Deck // by ticket ID
	panes     map[int64]paneRef        // by pane ID
	ticketsOf map[string]string        // ticket ID → project ID, for live counts

	overlay overlay
	status  string
	isErr   bool
	loading bool
}

// paneRef is what a pane runs: which task's deck holds it, which session.
type paneRef struct {
	ticketID  string
	sessionID string
}

// overlay is a modal over the task screen; at most one is open.
type overlay interface {
	update(m *Model, msg tea.Msg) tea.Cmd
	view(m Model, height int) string
}

// New builds the workbench over be; panes run the claude binary at claude.
func New(be Backend, claude string) Model {
	return Model{
		be:        be,
		claude:    claude,
		width:     80,
		height:    24,
		decks:     map[string]termpane.Deck{},
		panes:     map[int64]paneRef{},
		ticketsOf: map[string]string{},
	}
}

// Messages from backend calls.
type (
	projectsMsg struct {
		projects []domain.Project
		err      error
	}
	ticketsMsg struct {
		projectID string
		tickets   []domain.Ticket
		sessions  []domain.Session
		err       error
	}
	sessionsMsg struct {
		projectID string
		sessions  []domain.Session
		err       error
	}
	endedMsg struct{ err error }
)

// Messages from deck commands.
type (
	newSessionMsg   struct{}
	showSessionsMsg struct{}
	backMsg         struct{}
)

func (m Model) Init() tea.Cmd { return m.loadProjects() }

func call[T any](f func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return f(ctx)
}

func (m Model) loadProjects() tea.Cmd {
	be := m.be
	return func() tea.Msg {
		ps, err := call(be.ListProjects)
		return projectsMsg{projects: ps, err: err}
	}
}

func (m Model) loadTickets(projectID string) tea.Cmd {
	be := m.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()
		ts, err := be.ListTickets(ctx, projectID)
		if err != nil {
			return ticketsMsg{projectID: projectID, err: err}
		}
		ss, err := be.ListSessions(ctx, SessionFilter{ProjectID: projectID})
		return ticketsMsg{projectID: projectID, tickets: ts, sessions: ss, err: err}
	}
}

func (m Model) loadSessions(projectID string) tea.Cmd {
	be := m.be
	return func() tea.Msg {
		ss, err := call(func(ctx context.Context) ([]domain.Session, error) {
			return be.ListSessions(ctx, SessionFilter{ProjectID: projectID})
		})
		return sessionsMsg{projectID: projectID, sessions: ss, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		body := tea.WindowSizeMsg{Width: msg.Width, Height: max(1, msg.Height-headerHeight)}
		for id, d := range m.decks {
			m.decks[id], _ = d.Update(body)
		}
		return m, nil

	case projectsMsg:
		m.loading = false
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.projects = msg.projects
		m.projCursor = clamp(m.projCursor, len(m.projects))
		return m, nil

	case ticketsMsg:
		if msg.projectID != m.project.ID {
			return m, nil // the user moved on
		}
		m.loading = false
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.tickets = sortTickets(msg.tickets)
		m.sessions = msg.sessions
		for _, t := range m.tickets {
			m.ticketsOf[t.ID] = t.ProjectID
		}
		m.taskCursor = clamp(m.taskCursor, len(m.tickets))
		return m, nil

	case sessionsMsg:
		if msg.projectID == m.project.ID && msg.err == nil {
			m.sessions = msg.sessions
		}
		return m, nil

	case launchedMsg:
		return m.launched(msg)

	case endedMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		return m, m.loadSessions(m.project.ID)

	case termpane.FrameMsg:
		return m.routePane(msg.ID, msg)
	case termpane.ExitedMsg:
		return m.paneExited(msg)
	case termpane.ClosedMsg:
		return m.paneClosed(msg)

	case newSessionMsg:
		return m.openPicker()
	case showSessionsMsg:
		m.overlay = newSessionsOverlay()
		return m, m.loadSessions(m.project.ID)
	case backMsg:
		return m.back(), nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" && m.screen != screenTask {
			return m, tea.Quit
		}
		if m.overlay != nil {
			return m, m.overlay.update(&m, msg)
		}
		switch m.screen {
		case screenProjects:
			return m.projectsKey(msg)
		case screenTasks:
			return m.tasksKey(msg)
		default:
			return m.taskKey(msg)
		}

	case tea.PasteMsg:
		if m.overlay != nil {
			return m, m.overlay.update(&m, msg)
		}
		if m.screen == screenTask {
			return m.toDeck(msg)
		}
		return m, nil
	}

	if m.overlay != nil {
		return m, m.overlay.update(&m, msg)
	}
	return m, nil
}

// fail shows err in the header. An API error reads as its code's meaning.
func (m Model) fail(err error) Model {
	m.status, m.isErr = describe(err), true
	return m
}

func (m Model) note(s string) Model {
	m.status, m.isErr = s, false
	return m
}

func describe(err error) string {
	var api *APIError
	if errors.As(err, &api) {
		switch api.Code {
		case "SESSION_TRANSCRIPT_MISSING":
			return "claude has no saved conversation for that session any more"
		case "WORKSPACE_MISSING":
			return "that session's worktree was removed; it cannot be resumed"
		case "CLAUDE_CLI_NOT_FOUND":
			return api.Message
		case "BRANCH_EXISTS":
			return "a branch for this session already exists; try again"
		}
		return api.Message
	}
	return err.Error()
}

func (m Model) back() Model {
	m.overlay = nil
	m.status = ""
	switch m.screen {
	case screenTask:
		m.screen = screenTasks
		return m
	case screenTasks:
		m.screen = screenProjects
	}
	return m
}

// deck is the current task's deck, created on first use.
func (m *Model) deck() termpane.Deck {
	d, ok := m.decks[m.ticket.ID]
	if !ok {
		d = termpane.NewDeck(
			termpane.Command{Key: "c", Label: "new session", Msg: func() tea.Msg { return newSessionMsg{} }},
			termpane.Command{Key: "s", Label: "sessions", Msg: func() tea.Msg { return showSessionsMsg{} }},
			termpane.Command{Key: "b", Label: "back", Msg: func() tea.Msg { return backMsg{} }},
		)
		d, _ = d.Update(tea.WindowSizeMsg{Width: m.width, Height: max(1, m.height-headerHeight)})
		m.decks[m.ticket.ID] = d
	}
	return d
}

func (m Model) toDeck(msg tea.Msg) (tea.Model, tea.Cmd) {
	d, cmd := m.deck().Update(msg)
	m.decks[m.ticket.ID] = d
	return m, cmd
}

// routePane delivers a pane's message to whichever deck holds the pane, shown
// or not, so background agents keep drawing.
func (m Model) routePane(id int64, msg tea.Msg) (tea.Model, tea.Cmd) {
	ref, ok := m.panes[id]
	if !ok {
		return m, nil
	}
	d, cmd := m.decks[ref.ticketID].Update(msg)
	m.decks[ref.ticketID] = d
	return m, cmd
}

// paneExited ends the session of a pane whose claude quit on its own. A pane
// the user closed is no longer in its deck by then; ClosedMsg ends that one,
// as stopped rather than by exit code.
func (m Model) paneExited(msg termpane.ExitedMsg) (tea.Model, tea.Cmd) {
	ref, ok := m.panes[msg.ID]
	if !ok {
		return m, nil
	}
	next, cmd := m.routePane(msg.ID, msg)
	m = next.(Model)
	if !m.decks[ref.ticketID].Owns(msg.ID) {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.end(ref.sessionID, exitCode(msg.Err), false))
}

func (m Model) paneClosed(msg termpane.ClosedMsg) (tea.Model, tea.Cmd) {
	ref, ok := m.panes[msg.ID]
	if !ok {
		return m, nil
	}
	delete(m.panes, msg.ID)
	return m, m.end(ref.sessionID, 0, true)
}

func (m Model) end(sessionID string, code int, closed bool) tea.Cmd {
	be := m.be
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return endedMsg{err: be.EndSession(ctx, sessionID, code, closed)}
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

// Shutdown stops every pane and ends its session as closed. The host calls it
// after the program exits.
func (m Model) Shutdown(ctx context.Context) {
	for _, d := range m.decks {
		d.CloseAll()
	}
	for _, ref := range m.panes {
		_ = m.be.EndSession(ctx, ref.sessionID, 0, true)
	}
}

// liveIn counts the running panes of a task, or of every task of a project.
func (m Model) liveIn(ticketID string) int {
	if d, ok := m.decks[ticketID]; ok {
		return d.Live()
	}
	return 0
}

func (m Model) liveInProject(projectID string) int {
	n := 0
	for ticketID, d := range m.decks {
		if m.ticketsOf[ticketID] == projectID {
			n += d.Live()
		}
	}
	return n
}

func (m Model) View() tea.View {
	var body string
	var cursor *tea.Cursor
	bodyHeight := max(1, m.height-headerHeight)
	switch m.screen {
	case screenProjects:
		body = m.projectsView(bodyHeight)
	case screenTasks:
		body = m.tasksView(bodyHeight)
	default:
		body, cursor = m.taskView(bodyHeight)
	}
	if m.overlay != nil {
		body = m.overlay.view(m, bodyHeight)
		cursor = nil
	}
	if cursor != nil {
		cursor.Y += headerHeight
	}
	v := tea.NewView(m.header() + "\n" + body)
	v.AltScreen = true
	v.Cursor = cursor
	return v
}

func (m Model) header() string {
	crumbs := []string{headerStyle.Render("coding pool")}
	if m.screen >= screenTasks {
		crumbs = append(crumbs, m.project.Name)
	}
	if m.screen == screenTask {
		crumbs = append(crumbs, m.ticket.Title)
	}
	left := strings.Join(crumbs, crumbStyle.Render(" › "))

	var right string
	switch {
	case m.status != "" && m.isErr:
		right = errorStyle.Render(m.status)
	case m.status != "":
		right = noteStyle.Render(m.status)
	case m.loading:
		right = noteStyle.Render("loading…")
	}
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return ansi.Truncate(left+" "+right, m.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func clamp(i, n int) int {
	if n == 0 {
		return 0
	}
	return max(0, min(i, n-1))
}

// fit pads or truncates s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// window returns the [from, to) slice of n rows to show so cursor stays
// visible in height rows.
func window(cursor, n, height int) (int, int) {
	if n <= height {
		return 0, n
	}
	from := max(0, min(cursor-height/2, n-height))
	return from, from + height
}

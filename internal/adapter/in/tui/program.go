// Package tui is the terminal front-end: an inbound adapter that drives the
// application services through a Bubble Tea program.
//
// The root Model owns the chrome (nav rail, header, hint bar), the polling of
// the backend snapshot, the root-level modals and the set of open session
// screens. It delegates the body to one screen per section; see screens.go
// for the contract and doc.go for the message flow and the patterns in use.
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/screens/session"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// Options configures the program.
type Options struct {
	Backend backend.Backend
	Theme   theme.Theme
	// AutoTheme lets the terminal's reported background pick light or dark.
	AutoTheme bool
	// LogPath is the file the `d` key shows; slog writes there in cmd/tui.
	LogPath string
	// RefreshInterval paces list reloads; zero means two seconds.
	RefreshInterval time.Duration
}

const defaultRefreshInterval = 2 * time.Second

// Model is the root Bubble Tea model.
type Model struct {
	opts    Options
	th      theme.Theme
	width   int
	height  int
	section core.Section
	snap    backend.Snapshot

	// screens holds one body per section, indexed by core.Section.
	screens [core.SectionCount]screen

	// sessions keeps every opened session screen alive, shown or not, so its
	// subscription keeps receiving approvals in the background.
	sessions map[string]*session.Model
	// active is the session id shown instead of the section body, or "".
	active string

	// modal is the root overlay owning the keyboard, or nil.
	modal    modal
	showHelp bool
	toast    string
	toastErr bool
}

// New builds the root model; call Init through tea.NewProgram.
func New(opts Options) Model {
	if opts.RefreshInterval == 0 {
		opts.RefreshInterval = defaultRefreshInterval
	}
	m := Model{opts: opts, th: opts.Theme, sessions: map[string]*session.Model{}}
	m.screens = newScreens(m.bodyContext(), opts.Backend)
	return m
}

// NewProgram wraps the model in a program with the alt screen enabled.
func NewProgram(opts Options) *tea.Program {
	return tea.NewProgram(New(opts))
}

// Section is the active nav destination.
func (m Model) Section() core.Section { return m.section }

// Theme is the palette in use.
func (m Model) Theme() theme.Theme { return m.th }

// OpenSessions counts session screens kept alive, shown or in the background.
func (m Model) OpenSessions() int { return len(m.sessions) }

// Init loads the first snapshot, starts the refresh timer, and asks the
// terminal for its background when the theme is automatic.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.load(), m.tick()}
	if m.opts.AutoTheme {
		cmds = append(cmds, tea.RequestBackgroundColor)
	}
	return tea.Batch(cmds...)
}

func (m Model) load() tea.Cmd {
	be := m.opts.Backend
	return func() tea.Msg { return core.SnapshotMsg{Snapshot: backend.Load(be)} }
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.opts.RefreshInterval, func(time.Time) tea.Msg { return core.TickMsg{} })
}

// Update routes messages: root-level state first, then session-bound
// messages, then keys, then whatever the active screen wants.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.broadcast(core.ContextMsg{Ctx: m.bodyContext()})
	case tea.BackgroundColorMsg:
		if !m.opts.AutoTheme {
			return m, nil
		}
		m.th = theme.Light()
		if msg.IsDark() {
			m.th = theme.Dark()
		}
		return m.broadcast(core.ContextMsg{Ctx: m.bodyContext()})
	case core.TickMsg:
		return m, tea.Batch(m.load(), m.tick())
	case core.SnapshotMsg:
		m.snap = msg.Snapshot
		return m.broadcast(msg)
	case core.RefreshMsg:
		return m, m.load()
	case core.ToastMsg:
		m.toast, m.toastErr = msg.Text, msg.IsError
		return m, nil
	case core.NavigateMsg:
		return m.navigate(msg)
	case components.PaletteChosenMsg:
		m.modal = nil
		return m.navigate(navigateFor(msg.Item))
	case session.BackMsg:
		m.active = ""
		return m, nil
	case session.ClosedMsg:
		return m.dropSession(msg.ID), nil
	case session.Routed:
		return m.routeToSession(msg.SessionRef(), msg)
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	// Anything else (form, confirm and editor results, write outcomes) belongs
	// to whoever is on screen.
	return m.forward(msg)
}

// navigate switches section and/or opens a session.
func (m Model) navigate(n core.NavigateMsg) (tea.Model, tea.Cmd) {
	if n.HasSection {
		m.section = n.Section
	}
	if n.SessionID != "" {
		return m.openSession(n.SessionID)
	}
	return m, nil
}

// broadcast sends a message to every screen, including open sessions.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for id, s := range m.sessions {
		next, cmd := s.Update(msg)
		m.sessions[id] = &next
		cmds = append(cmds, cmd)
	}
	for i := range m.screens {
		var cmd tea.Cmd
		m.screens[i], cmd = m.screens[i].Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// forward sends a message to the active screen only: the shown session if
// one is open, else the section body.
func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.active != "" {
		return m.routeToSession(m.active, msg)
	}
	var cmd tea.Cmd
	m.screens[m.section], cmd = m.screens[m.section].Update(msg)
	return m, cmd
}

// current is the body of the active section.
func (m Model) current() screen { return m.screens[m.section] }

// key resolves who owns the keyboard, from the outermost layer inwards: a
// root modal, then the shown session, then a screen that is capturing text,
// and only then the global bindings.
func (m Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.toast = ""
	if m.modal != nil {
		return m.modal.key(m, k)
	}
	if k.String() == "ctrl+c" {
		return m.quit()
	}
	if m.active != "" {
		return m.routeToSession(m.active, k)
	}
	if m.current().Capturing() {
		return m.forward(k)
	}
	return m.globalKey(k)
}

// globalKey handles the bindings that work on every section.
func (m Model) globalKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		if live := m.liveSessions(); live > 0 {
			m.modal = quitPrompt{live: live}
			return m, nil
		}
		return m.quit()
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "ctrl+k":
		m.modal = newPaletteModal(m.th, m.paletteItems())
		return m, nil
	case "d":
		m.modal = newLogModal(m.opts.LogPath, m.bodyContext())
		return m, nil
	case "r":
		return m, m.load()
	case "tab":
		m.section = m.section.Next()
		return m, nil
	case "shift+tab":
		m.section = m.section.Prev()
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7":
		m.section = core.Section(k.String()[0] - '1')
		return m, nil
	}
	return m.forward(k)
}

// quit closes every subscription and ends the program.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.closeSessions()
	return m, tea.Quit
}

// bodyContext is the size and theme of the area a screen draws into: the
// window minus the nav rail, the header line and the hint bar.
func (m Model) bodyContext() core.Context {
	w := max(m.width-m.navWidth()-3, 20)
	h := max(m.height-4, 5)
	return core.Context{Theme: m.th, Width: w, Height: h}
}

// View composes rail | header + body, then the hint bar. A modal replaces the
// body while it is open.
func (m Model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading…")
	}
	ctx := m.bodyContext()
	body := m.body()
	if m.modal != nil {
		body = m.modal.view(m, ctx)
	}
	main := lipgloss.JoinVertical(lipgloss.Left,
		m.header(ctx.Width),
		lipgloss.NewStyle().Width(ctx.Width).Height(ctx.Height).Render(body),
	)
	top := lipgloss.JoinHorizontal(lipgloss.Top, m.rail(ctx.Height+2), " ", main)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, top, m.hintBar()))
	v.BackgroundColor = m.th.Bg
	v.AltScreen = true
	v.WindowTitle = "coding_pool"
	return v
}

func (m Model) body() string {
	if s, ok := m.shownSession(); ok {
		return s.View()
	}
	return m.current().View()
}

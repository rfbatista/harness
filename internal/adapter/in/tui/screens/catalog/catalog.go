// Package catalog is the list screen shared by Agents, Skills, MCP servers and
// Projects (Settings). The screen owns what every list has in common: the
// cursor, the `/` filter, the empty state, the overlay slot and the drill-in
// slot. Each Kind plugs in a spec with what differs: columns and rows, the
// keys that open forms, and any async outcome it wants to react to.
package catalog

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/components"
	"operators-mcp/internal/adapter/in/tui/core"
)

// Kind selects which catalog the screen lists.
type Kind int

const (
	Agents Kind = iota
	Skills
	MCPs
	Projects
)

// entry is one listed item: its id plus the cells the table shows.
type entry struct {
	id    string
	cells []string
}

// spec is the Strategy for one Kind: everything that differs between the four
// catalogs. The Model calls it; it never runs on its own.
//
// Writes are not part of the contract. A spec opens a form or confirm through
// m.overlays and hands over the command to run on accept, so the write lives
// next to the key that asked for it.
type spec interface {
	columns() []components.Column
	entries(m *Model) []entry
	summary() string
	emptyText() string
	hints() []core.KeyHint
	// key handles kind-specific list keys; handled=false lets the generic
	// list keys (movement, filter) run.
	key(m *Model, k tea.KeyPressMsg) (cmd tea.Cmd, handled bool)
	// result handles kind-specific async outcomes (probes, validations);
	// handled=false means the message is not one of its own.
	result(m *Model, msg tea.Msg) (cmd tea.Cmd, handled bool)
	// conflict is asked for a follow-up when a write failed with a code that a
	// forced retry would fix. It reports whether it offered one, usually by
	// opening a confirm; false closes the overlay with an error toast.
	conflict(m *Model, op components.OpDoneMsg) bool
}

func specFor(k Kind) spec {
	switch k {
	case Agents:
		return agentsSpec{}
	case Skills:
		return skillsSpec{}
	case MCPs:
		return mcpsSpec{}
	}
	return projectsSpec{}
}

// detail is a drill-in that replaces the list until it emits closeDetailMsg.
type detail interface {
	update(msg tea.Msg) (detail, tea.Cmd)
	view() string
	capturing() bool
	hints() []core.KeyHint
}

// closeDetailMsg asks the list to drop its drill-in.
type closeDetailMsg struct{}

func closeDetail() tea.Msg { return closeDetailMsg{} }

// Model is the catalog state.
type Model struct {
	ctx       core.Context
	kind      Kind
	spec      spec
	be        backend.Backend
	snap      backend.Snapshot
	all       []entry
	shown     []entry
	table     components.Table
	filter    textinput.Model
	filtering bool
	query     string

	overlays components.Overlays
	detail   detail
}

// New builds a catalog screen for one kind over a backend.
func New(ctx core.Context, kind Kind, be backend.Backend) Model {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "filter"
	m := Model{ctx: ctx, kind: kind, be: be, filter: in, spec: specFor(kind)}
	m.table = components.NewTable(m.spec.columns())
	return m
}

// Selected is the id under the cursor, or "".
func (m Model) Selected() string {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.shown) {
		return ""
	}
	return m.shown[i].id
}

// Capturing reports whether a text input, overlay or drill-in owns the
// keyboard, so the root must not act on global keys.
func (m Model) Capturing() bool {
	if m.detail != nil {
		return true
	}
	return m.filtering || m.overlays.Active()
}

// Hints lists the keys this screen answers to.
func (m Model) Hints() []core.KeyHint {
	if m.detail != nil {
		return m.detail.hints()
	}
	return append(m.spec.hints(), core.KeyHint{Key: "j/k", Desc: "move"}, core.KeyHint{Key: "/", Desc: "filter"})
}

// Summary is the chrome subtitle.
func (m Model) Summary() string { return m.spec.summary() }

// Update routes a message through the layers of the screen, outermost first:
// context and snapshot reach everyone; a drill-in owns everything else while
// open; then the overlay; then the list itself.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case core.ContextMsg:
		m.ctx = msg.Ctx
		return m.forwardToDetail(msg)
	case core.SnapshotMsg:
		m.snap = msg.Snapshot
		m.all = m.spec.entries(&m)
		m.apply()
		return m.forwardToDetail(msg)
	case closeDetailMsg:
		m.detail = nil
		return m, nil
	}
	if m.detail != nil {
		return m.forwardToDetail(msg)
	}
	if cmd, handled := m.overlays.Update(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case components.OpDoneMsg:
		return m.opDone(msg)
	case tea.KeyPressMsg:
		if m.filtering {
			return m.filterKey(msg)
		}
		if cmd, handled := m.spec.key(&m, msg); handled {
			return m, cmd
		}
		return m.key(msg)
	}
	if cmd, handled := m.spec.result(&m, msg); handled {
		return m, cmd
	}
	return m, nil
}

func (m Model) forwardToDetail(msg tea.Msg) (Model, tea.Cmd) {
	if m.detail == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.update(msg)
	return m, cmd
}

// opDone settles a write; a conflict gets one chance at a forced retry.
func (m Model) opDone(msg components.OpDoneMsg) (Model, tea.Cmd) {
	cmd, conflict := m.overlays.Done(msg)
	if !conflict {
		return m, cmd
	}
	if m.spec.conflict(&m, msg) {
		return m, nil
	}
	m.overlays.Close()
	return m, components.ErrorToast(core.Message(msg.Err))
}

func (m Model) filterKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		m.filtering = false
		m.filter.Blur()
		m.query = m.filter.Value()
		m.apply()
		return m, nil
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.query = ""
		m.apply()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.query = m.filter.Value()
	m.apply()
	return m, cmd
}

// key handles the generic list keys shared by every kind.
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
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "esc":
		if m.query != "" {
			m.query = ""
			m.filter.SetValue("")
			m.apply()
		}
	}
	return m, nil
}

// apply filters all → shown on the query and refreshes the table.
func (m *Model) apply() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	m.shown = m.shown[:0]
	for _, e := range m.all {
		if q == "" || strings.Contains(strings.ToLower(strings.Join(e.cells, " ")), q) {
			m.shown = append(m.shown, e)
		}
	}
	rows := make([][]string, len(m.shown))
	for i, e := range m.shown {
		rows[i] = e.cells
	}
	m.table.SetRows(rows)
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// View draws the detail, or the filter line, table and footer with any
// overlay placed over the body.
func (m Model) View() string {
	if m.detail != nil {
		return m.detail.view()
	}
	th := m.ctx.Theme
	var parts []string
	if m.filtering || m.query != "" {
		parts = append(parts, m.filter.View())
	}
	footer := m.footer()
	h := max(m.ctx.Height-len(parts)-2, 3)
	if len(m.shown) == 0 {
		text := m.spec.emptyText()
		if m.query != "" {
			text = "No matches for " + fmt.Sprintf("%q", m.query)
		}
		parts = append(parts, th.Subtitle().Render(text))
	} else {
		parts = append(parts, m.table.View(th, m.ctx.Width, h))
	}
	if footer != "" {
		parts = append(parts, "", footer)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return m.withOverlay(body)
}

// withOverlay places the open form or confirm over a rendered body.
func (m Model) withOverlay(body string) string {
	if !m.overlays.Active() {
		return body
	}
	return components.OverlayOn(body, m.overlays.View(overlayWidth(m.ctx)), m.ctx.Width, m.ctx.Height)
}

// overlayWidth fits a form or confirm to the body, capped for readability.
func overlayWidth(ctx core.Context) int { return min(ctx.Width-4, 80) }

func (m Model) footer() string {
	if m.kind != Projects {
		return ""
	}
	th := m.ctx.Theme
	root := m.snap.Settings["workspaces.root"]
	if root == "" {
		root = "~/.coding-pool/worktrees (default)"
	}
	return th.Meta().Render("Workspaces root: ") + th.Subtitle().Render(root) + th.Meta().Render("   (w to change)")
}

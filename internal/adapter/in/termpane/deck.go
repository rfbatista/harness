package termpane

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// DefaultPrefix is the key that starts a deck command, tmux-style: press it,
// then a command key. Every other key goes to the focused pane.
const DefaultPrefix = "ctrl+]"

const (
	tabBarHeight = 1
	maxLabel     = 24
)

var (
	tabActive = lipgloss.NewStyle().Reverse(true).Bold(true)
	tabIdle   = lipgloss.NewStyle()
	tabDead   = lipgloss.NewStyle().Faint(true)
	barHint   = lipgloss.NewStyle().Faint(true)
	barNotice = lipgloss.NewStyle().Bold(true)
)

// Command is a host-defined deck command: after the prefix, Key sends Msg()
// to the host. Label is its wording in the command hint.
type Command struct {
	Key   string
	Label string
	Msg   func() tea.Msg
}

// ClosedMsg says the user closed a pane with the x command. The deck has
// already stopped its process; the host learns so it can clean up after it.
type ClosedMsg struct{ ID int64 }

// Deck shows several panes one at a time, with a tab bar listing all of them:
// which one is focused, which have new output you haven't seen (●), and which
// have exited (✓ clean, ✗ with an error).
//
// The deck does not create panes; the host starts them and calls Add. Built-in
// commands, after the prefix key:
//
//	n / →  next pane           x    close the focused pane
//	p / ←  previous pane       q    quit
//	1-9    jump to a pane      prefix  send the prefix key itself to the pane
//
// Host commands (see Command) come first in the hint.
type Deck struct {
	prefix   string
	commands []Command

	tabs   []tab
	active int
	width  int
	height int

	awaiting bool   // the prefix was pressed; the next key is a command
	notice   string // one-shot message in the tab bar
}

type tab struct {
	pane   Model
	unseen bool // output arrived while another pane was focused
}

// NewDeck builds an empty deck with the host's extra commands.
func NewDeck(commands ...Command) Deck {
	return Deck{prefix: DefaultPrefix, commands: commands, width: 80, height: 24}
}

// WithPrefix changes the command prefix key, e.g. "ctrl+b".
func (d Deck) WithPrefix(key string) Deck {
	d.prefix = key
	return d
}

// Prefix is the key that starts a command.
func (d Deck) Prefix() string { return d.prefix }

// Add shows a started pane as the newest tab and focuses it.
func (d Deck) Add(p Model) (Deck, tea.Cmd) {
	p.Resize(d.paneSize())
	d.tabs = append(d.tabs, tab{pane: p})
	d.active = len(d.tabs) - 1
	return d, p.Init()
}

// Focus shows the pane with the given ID, if the deck holds it.
func (d Deck) Focus(id int64) Deck {
	for i, t := range d.tabs {
		if t.pane.ID() == id {
			return d.focus(i)
		}
	}
	return d
}

// Owns reports whether the pane with the given ID is in this deck.
func (d Deck) Owns(id int64) bool {
	for _, t := range d.tabs {
		if t.pane.ID() == id {
			return true
		}
	}
	return false
}

// Panes lists the deck's panes in tab order.
func (d Deck) Panes() []Model {
	out := make([]Model, len(d.tabs))
	for i, t := range d.tabs {
		out[i] = t.pane
	}
	return out
}

// Live counts the panes whose process is still running.
func (d Deck) Live() int {
	n := 0
	for _, t := range d.tabs {
		if exited, _ := t.pane.Exited(); !exited {
			n++
		}
	}
	return n
}

// Len is the number of panes.
func (d Deck) Len() int { return len(d.tabs) }

// Active is the index of the focused pane.
func (d Deck) Active() int { return d.active }

// Awaiting reports whether the prefix was pressed and a command key is due.
func (d Deck) Awaiting() bool { return d.awaiting }

// Notify shows a one-shot message in the tab bar.
func (d Deck) Notify(msg string) Deck {
	d.notice = msg
	return d
}

// Update routes pane messages by pane ID, runs deck commands, and sends every
// other key to the focused pane.
func (d Deck) Update(msg tea.Msg) (Deck, tea.Cmd) {
	switch msg := msg.(type) {
	case FrameMsg:
		return d.route(msg.ID, msg)
	case ExitedMsg:
		return d.route(msg.ID, msg)
	case tea.WindowSizeMsg:
		d.width, d.height = msg.Width, msg.Height
		for i := range d.tabs {
			d.tabs[i].pane.Resize(d.paneSize())
		}
		return d, nil
	case tea.KeyPressMsg:
		return d.key(msg)
	case tea.PasteMsg:
		return d.forward(msg)
	}
	return d, nil
}

func (d Deck) key(msg tea.KeyPressMsg) (Deck, tea.Cmd) {
	k := msg.String()
	if !d.awaiting {
		if k == d.prefix {
			d.awaiting, d.notice = true, ""
			return d, nil
		}
		return d.forward(msg)
	}

	d.awaiting = false
	for _, c := range d.commands {
		if c.Key == k {
			return d, c.Msg
		}
	}
	switch k {
	case "n", "right", "tab":
		return d.focus(d.active + 1), nil
	case "p", "left", "shift+tab":
		return d.focus(d.active - 1 + len(d.tabs)), nil
	case "x":
		return d.closeActive()
	case "q":
		return d, tea.Quit
	case d.prefix:
		return d.forward(msg)
	case "esc":
		return d, nil
	}
	if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
		if i := int(k[0] - '1'); i < len(d.tabs) {
			return d.focus(i), nil
		}
		d.notice = "no pane " + k
		return d, nil
	}
	d.notice = fmt.Sprintf("unknown command %q", k)
	return d, nil
}

func (d Deck) focus(i int) Deck {
	if len(d.tabs) == 0 {
		return d
	}
	d.active = i % len(d.tabs)
	d.tabs[d.active].unseen = false
	return d
}

// closeActive drops the focused pane now and stops its process off the UI
// goroutine, since Close can wait for the child to die; then it tells the
// host with ClosedMsg.
func (d Deck) closeActive() (Deck, tea.Cmd) {
	if len(d.tabs) == 0 {
		return d, nil
	}
	p := d.tabs[d.active].pane
	d.tabs = append(d.tabs[:d.active:d.active], d.tabs[d.active+1:]...)
	stop := func() tea.Msg { _ = p.Close(); return ClosedMsg{ID: p.ID()} }
	if len(d.tabs) == 0 {
		d.active = 0
		return d, stop
	}
	return d.focus(min(d.active, len(d.tabs)-1)), stop
}

// CloseAll stops every pane's process, in parallel. Hosts call it after the
// program ends so no child outlives them.
func (d Deck) CloseAll() {
	var wg sync.WaitGroup
	for _, t := range d.tabs {
		wg.Go(func() { _ = t.pane.Close() })
	}
	wg.Wait()
}

func (d Deck) route(id int64, msg tea.Msg) (Deck, tea.Cmd) {
	for i := range d.tabs {
		if d.tabs[i].pane.ID() != id {
			continue
		}
		if i != d.active {
			d.tabs[i].unseen = true
		}
		var cmd tea.Cmd
		d.tabs[i].pane, cmd = d.tabs[i].pane.Update(msg)
		return d, cmd
	}
	return d, nil // from a pane that was already closed
}

func (d Deck) forward(msg tea.Msg) (Deck, tea.Cmd) {
	if len(d.tabs) == 0 {
		return d, nil
	}
	var cmd tea.Cmd
	d.tabs[d.active].pane, cmd = d.tabs[d.active].pane.Update(msg)
	return d, cmd
}

// PaneSize is the size a pane gets in this deck: its area minus the tab bar.
func (d Deck) PaneSize() (int, int) { return d.paneSize() }

func (d Deck) paneSize() (int, int) {
	return d.width, max(1, d.height-tabBarHeight)
}

// View is the tab bar over the focused pane; with no panes, only the tab bar.
func (d Deck) View() string {
	if len(d.tabs) == 0 {
		return d.tabBar()
	}
	return d.tabBar() + "\n" + d.tabs[d.active].pane.View()
}

// Cursor is the focused pane's cursor in deck coordinates. It is hidden while
// a command is pending, so the prefix reads as a mode.
func (d Deck) Cursor() *tea.Cursor {
	if len(d.tabs) == 0 || d.awaiting {
		return nil
	}
	c := d.tabs[d.active].pane.Cursor()
	if c != nil {
		c.Y += tabBarHeight
	}
	return c
}

func (d Deck) tabBar() string {
	var b strings.Builder
	for i, t := range d.tabs {
		b.WriteString(d.tabLabel(i, t))
	}

	var right string
	switch {
	case d.notice != "":
		right = barNotice.Render(d.notice)
	case d.awaiting:
		right = barHint.Render(d.hint())
	default:
		right = barHint.Render(d.prefix + " for commands")
	}

	left := b.String()
	gap := d.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return ansi.Truncate(left+" "+right, d.width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (d Deck) hint() string {
	parts := make([]string, 0, len(d.commands)+4)
	for _, c := range d.commands {
		parts = append(parts, c.Key+" "+c.Label)
	}
	return strings.Join(append(parts, "n/p switch", "1-9 jump", "x close", "q quit"), " · ")
}

func (d Deck) tabLabel(i int, t tab) string {
	label := ansi.Truncate(strings.TrimSpace(t.pane.Title()), maxLabel, "…")
	exited, err := t.pane.Exited()
	var mark string
	switch {
	case exited && err != nil:
		mark = " ✗"
	case exited:
		mark = " ✓"
	case t.unseen:
		mark = " ●"
	}
	text := fmt.Sprintf(" %d %s%s ", i+1, label, mark)
	switch {
	case i == d.active:
		return tabActive.Render(text)
	case exited:
		return tabDead.Render(text)
	default:
		return tabIdle.Render(text)
	}
}

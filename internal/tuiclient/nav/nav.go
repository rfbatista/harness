// Package nav is the contract between tui-client's root and its screens: the
// Screen interface, and the messages a screen returns to move through the
// stack or put a line in the header. Screens import nav, never the root or
// each other; a screen that opens another is handed a constructor for it.
package nav

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/tuiclient/ui"
)

// Screen is one body the root stacks: projects, a project's tasks, a task.
type Screen interface {
	// Init is run when the screen is pushed.
	Init() tea.Cmd
	// Update gets key presses and pastes while the screen is on top, and
	// every other message whether it is on top or not, so a list underneath
	// stays current. A screen ignores messages that are not its own.
	Update(tea.Msg) (Screen, tea.Cmd)
	// View draws the screen into width × height cells; the cursor, when set,
	// is relative to the screen's top-left corner.
	View(width, height int) (string, *tea.Cursor)
	// Crumb is the screen's segment of the header's breadcrumb.
	Crumb() string
	// Capturing screens get every key, ctrl+c included: the task screen hands
	// them to claude.
	Capturing() bool
}

// ProjectScoped is a screen inside one project. The root follows the live
// session feed of the project the deepest such screen is in.
type ProjectScoped interface {
	ProjectID() string
}

type (
	// PushMsg puts a screen on top of the stack.
	PushMsg struct{ Screen Screen }
	// PopMsg returns to the screen underneath.
	PopMsg struct{}
	// StatusMsg sets the header's status line; Err marks it as an error.
	StatusMsg struct {
		Text string
		Err  bool
	}
)

// Push opens s on top of the current screen.
func Push(s Screen) tea.Cmd { return func() tea.Msg { return PushMsg{Screen: s} } }

// Pop goes back one screen.
func Pop() tea.Cmd { return func() tea.Msg { return PopMsg{} } }

// Note shows text in the header.
func Note(text string) tea.Cmd { return func() tea.Msg { return StatusMsg{Text: text} } }

// Fail shows err in the header, as ui.Describe reads it.
func Fail(err error) tea.Cmd {
	return func() tea.Msg { return StatusMsg{Text: ui.Describe(err), Err: true} }
}

// CallTimeout bounds one backend call. Starting a session runs `git worktree
// add`, which can take a while on a large repository.
const CallTimeout = 2 * time.Minute

// Call runs f with a CallTimeout context and delivers its message.
func Call(f func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), CallTimeout)
		defer cancel()
		return f(ctx)
	}
}

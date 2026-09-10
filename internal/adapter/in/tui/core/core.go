// Package core holds the vocabulary shared by the root program and every
// screen: sections, the render context, and the messages that cross the
// screen boundary. Screens import core; core imports no screen.
//
// Messages flow in two directions. The root pushes ContextMsg and SnapshotMsg
// down to every screen; screens send NavigateMsg, RefreshMsg and ToastMsg up
// to the root as tea.Cmd results. Nothing else crosses the boundary.
package core

import (
	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/theme"
)

// Section is one nav destination, in rail order.
type Section int

const (
	SectionTasks Section = iota
	SectionInbox
	SectionHistory
	SectionAgents
	SectionSkills
	SectionMCPs
	SectionSettings
	// SectionCount is how many sections the rail shows. Arrays indexed by
	// Section are sized with it.
	SectionCount
)

// Sections lists every section in rail order.
func Sections() []Section {
	out := make([]Section, 0, SectionCount)
	for s := Section(0); s < SectionCount; s++ {
		out = append(out, s)
	}
	return out
}

// Next wraps forward through the rail.
func (s Section) Next() Section { return (s + 1) % SectionCount }

// Prev wraps backward through the rail.
func (s Section) Prev() Section { return (s + SectionCount - 1) % SectionCount }

// Label is the rail wording.
func (s Section) Label() string {
	switch s {
	case SectionTasks:
		return "Tasks"
	case SectionInbox:
		return "Inbox"
	case SectionHistory:
		return "History"
	case SectionAgents:
		return "Agents"
	case SectionSkills:
		return "Skills"
	case SectionMCPs:
		return "MCP"
	case SectionSettings:
		return "Settings"
	}
	return ""
}

// EntityKind names the kind of thing a palette item points at. Its string
// form is what the palette shows and matches against.
type EntityKind string

const (
	KindTask      EntityKind = "task"
	KindSession   EntityKind = "session"
	KindAgent     EntityKind = "agent"
	KindSkill     EntityKind = "skill"
	KindMCPServer EntityKind = "mcp"
	KindProject   EntityKind = "project"
)

// Section is the rail destination that lists this kind of entity.
func (k EntityKind) Section() Section {
	switch k {
	case KindAgent:
		return SectionAgents
	case KindSkill:
		return SectionSkills
	case KindMCPServer:
		return SectionMCPs
	case KindProject:
		return SectionSettings
	}
	return SectionTasks
}

// Context is what a screen needs to draw: the theme and the body size it owns.
type Context struct {
	Theme  theme.Theme
	Width  int
	Height int
}

// ContextMsg delivers a new Context (resize or theme change) to a screen.
type ContextMsg struct{ Ctx Context }

// SnapshotMsg delivers a fresh backend read to every screen.
type SnapshotMsg struct{ Snapshot backend.Snapshot }

// TickMsg fires the periodic list refresh.
type TickMsg struct{}

// NavigateMsg asks the root to show something: a section, a live session, or
// both. HasSection false keeps the current section.
type NavigateMsg struct {
	Section    Section
	HasSection bool
	// SessionID, when set, opens that session's screen over the section.
	SessionID string
}

// ToastMsg shows a transient line in the chrome.
type ToastMsg struct {
	Text    string
	IsError bool
}

// KeyHint is one entry of the bottom hint bar.
type KeyHint struct {
	Key  string
	Desc string
}

// RefreshMsg asks the root to reload the snapshot now, after a write.
type RefreshMsg struct{}

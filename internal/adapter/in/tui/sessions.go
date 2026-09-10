package tui

import (
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/screens/session"
)

// Session screens are the one place the root keeps pointers: a session's
// subscription must outlive the screen being shown, so the root owns the
// map and routes background messages to the right screen by id.

// openSession shows a session screen, creating and subscribing it on first use.
func (m Model) openSession(id string) (tea.Model, tea.Cmd) {
	m.active = id
	if _, ok := m.sessions[id]; ok {
		return m, nil
	}
	s := session.New(m.bodyContext(), m.opts.Backend, m.snap, id)
	m.sessions[id] = &s
	return m, s.Init()
}

// shownSession is the session screen on display, if any.
func (m Model) shownSession() (*session.Model, bool) {
	if m.active == "" {
		return nil, false
	}
	s, ok := m.sessions[m.active]
	return s, ok
}

// routeToSession delivers a message to one session screen, shown or not.
func (m Model) routeToSession(id string, msg tea.Msg) (tea.Model, tea.Cmd) {
	s, ok := m.sessions[id]
	if !ok {
		return m, nil
	}
	next, cmd := s.Update(msg)
	m.sessions[id] = &next
	return m, cmd
}

// dropSession forgets a deleted session and returns to the section body if it
// was the one shown.
func (m Model) dropSession(id string) Model {
	if s, ok := m.sessions[id]; ok {
		s.Close()
		delete(m.sessions, id)
	}
	if m.active == id {
		m.active = ""
	}
	return m
}

// closeSessions cancels every session subscription before the program exits.
func (m Model) closeSessions() {
	for _, s := range m.sessions {
		s.Close()
	}
}

// liveSessions counts sessions the snapshot still reports as not finished.
func (m Model) liveSessions() int {
	n := 0
	for _, s := range m.snap.Sessions {
		if !s.Status.IsTerminal() {
			n++
		}
	}
	return n
}

// Package tui is the terminal front-end of coding_pool. This file is the map
// for a developer arriving from outside; docs/TUI.md has the longer version.
//
// # Where it sits
//
// The TUI is an inbound adapter in the hexagonal layout: it calls the
// application services (blueprint, planning, orchestration, workspaces) and
// never touches persistence or HTTP. cmd/tui boots the whole server graph
// (internal/app.NewTUI) and runs the Bubble Tea program on top of it, so the
// claude CLI's loopback endpoints keep working while the terminal is in use.
//
// # Packages
//
//	tui              root model: chrome, section screens, modals, sessions
//	tui/core         vocabulary every screen shares: Section, Context, messages
//	tui/backend      the Backend port, its production adapter and the Fake
//	tui/theme        palette and lipgloss styles, light and dark
//	tui/components   widgets: Form, Confirm, Overlays, Table, Palette, editor
//	tui/rollup       pure fold of sessions into task-level status and counts
//	tui/screens/*    one package per screen: dashboard, catalog, spawn, session
//
// Dependencies point one way: screens import components, core, rollup,
// theme and backend; core imports backend and theme; nothing imports a
// screen except the root. Screens never import each other, with one
// exception: dashboard embeds the spawn wizard.
//
// # Message flow
//
// The root polls backend.Load every RefreshInterval and broadcasts the
// result as core.SnapshotMsg to every screen, so list screens never read the
// backend themselves. Writes go the other way: a screen returns a tea.Cmd
// built with components.Op, the outcome comes back as components.OpDoneMsg,
// and the screen answers with core.RefreshMsg (reload now) or core.ToastMsg
// (show a line in the header). core.NavigateMsg asks the root to change
// section or open a session.
//
// The session screen is the one exception to polling: it subscribes to
// orchestration.Service and re-arms a blocking tea.Cmd per event. Its
// messages implement session.Routed so the root can deliver them to a
// screen that is open in the background.
//
// # Keyboard ownership
//
// Model.key resolves who gets a key press, outermost layer first: a root
// modal (quit prompt, palette, log), then the shown session, then a screen
// that reports Capturing (a text input, form or drill-in is open), and only
// then the global bindings. Every screen applies the same rule internally:
// drill-in, then overlay, then its own keys.
//
// # Patterns, and where to look
//
//	Strategy   screen (screens.go): the root swaps one body per section.
//	           catalog.spec: what differs between Agents, Skills, MCP, Projects.
//	Adapter    dashboardScreen / catalogScreen wrap concrete models into screen.
//	           backend.Services wraps the application services into Backend.
//	Facade     backend.Backend: one interface, composed of small role
//	           interfaces, over four services.
//	Command    components.Overlays: opening a form or confirm hands over the
//	           command to run on accept; tea.Cmd itself is a Command.
//	State      modal (modals.go) for the root overlays; session.barState for
//	           the intervene bar, one type per mode.
//	Composite  every Model owns child models and forwards messages to them.
//	Observer   session.Model.wait over orchestration.Service.Subscribe.
//	Null-ish   backend.Fake stands in for the services in tests.
//
// # Adding things
//
// A new section: add the Section to core, a screen package with New/Update/
// View/Hints/Summary/Capturing, an adapter in screens.go, an entry in
// newScreens, an icon in chrome.go and a badge in Model.count.
//
// A new catalog kind: add a Kind and a spec in screens/catalog; forms open
// through m.overlays.OpenForm with the write as the submit command.
//
// A new intervene-bar mode: add a barState in screens/session and return it
// from Model.bar.
//
// A new backend capability: add it to the role interface it belongs to in
// backend.Backend, implement it on Services and Fake.
package tui

// Package projects is tui-client's first screen: every project, with a count
// of the agents running in it.
package projects

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/panes"
	"operators-mcp/internal/tuiclient/ui"
)

// Screen lists projects; enter opens one with Open.
type Screen struct {
	projects ports.ProjectReader
	panes    *panes.Registry
	open     func(*domain.Project) nav.Screen

	list    []*domain.Project
	cursor  int
	loading bool
}

// New returns the projects screen. open builds the screen a chosen project
// opens into.
func New(projects ports.ProjectReader, reg *panes.Registry, open func(*domain.Project) nav.Screen) *Screen {
	return &Screen{projects: projects, panes: reg, open: open, loading: true}
}

type loadedMsg struct {
	projects []*domain.Project
	err      error
}

func (s *Screen) load() tea.Cmd {
	projects := s.projects
	return nav.Call(func(ctx context.Context) tea.Msg {
		ps, err := projects.ListProjects(ctx)
		return loadedMsg{projects: ps, err: err}
	})
}

func (s *Screen) Init() tea.Cmd { return s.load() }

func (s *Screen) Crumb() string { return "" }

func (s *Screen) Capturing() bool { return false }

func (s *Screen) Update(msg tea.Msg) (nav.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loadedMsg:
		s.loading = false
		if msg.err != nil {
			return s, nav.Fail(msg.err)
		}
		s.list = msg.projects
		s.cursor = ui.Clamp(s.cursor, len(s.list))
	case tea.KeyPressMsg:
		return s.key(msg)
	}
	return s, nil
}

func (s *Screen) key(msg tea.KeyPressMsg) (nav.Screen, tea.Cmd) {
	k := msg.String()
	if c, ok := ui.MoveCursor(k, s.cursor, len(s.list)); ok {
		s.cursor = c
		return s, nil
	}
	switch k {
	case "q":
		return s, tea.Quit
	case "r":
		s.loading = true
		return s, s.load()
	case "enter", "right", "l":
		if len(s.list) == 0 {
			return s, nil
		}
		return s, nav.Push(s.open(s.list[s.cursor]))
	}
	return s, nil
}

func (s *Screen) View(width, height int) (string, *tea.Cursor) {
	if len(s.list) == 0 {
		if s.loading {
			return ui.Note.Render("  loading…"), nil
		}
		return ui.Note.Render("  No projects yet. Create one in coding_pool first. (r to refresh, q to quit)"), nil
	}
	rows := make([]string, 0, height)
	from, to := ui.Window(s.cursor, len(s.list), height-1)
	nameW := min(32, max(12, width/3))
	for i := from; i < to; i++ {
		p := s.list[i]
		live := ""
		if n := s.panes.LiveInProject(p.ID); n > 0 {
			live = ui.Live.Render(fmt.Sprintf("● %d live", n))
		}
		row := " " + ui.Fit(p.Name, nameW) + "  " + ui.Fit(ui.Dim.Render(p.RootDir), max(0, width-nameW-14)) + " " + live
		rows = append(rows, ui.Row(row, i == s.cursor, width))
	}
	rows = append(rows, ui.Note.Render(" enter open · r refresh · q quit"))
	return strings.Join(rows, "\n"), nil
}

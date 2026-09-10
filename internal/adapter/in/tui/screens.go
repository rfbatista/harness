package tui

import (
	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/tui/backend"
	"operators-mcp/internal/adapter/in/tui/core"
	"operators-mcp/internal/adapter/in/tui/rollup"
	"operators-mcp/internal/adapter/in/tui/screens/catalog"
	"operators-mcp/internal/adapter/in/tui/screens/dashboard"
)

// screen is what the root asks of a section body. It is the Strategy the root
// swaps when the section changes: the root never knows whether a section is a
// dashboard, a catalog or something added later.
//
// Update returns the screen itself so the root can store it back without
// knowing its concrete type. Screens are value types; a screen that keeps
// pointers must document why.
type screen interface {
	Update(msg tea.Msg) (screen, tea.Cmd)
	View() string
	// Hints lists the keys the hint bar shows for this screen.
	Hints() []core.KeyHint
	// Summary is the subtitle shown next to the section title.
	Summary() string
	// Capturing reports that a text input or overlay owns the keyboard, so the
	// root must not act on global keys.
	Capturing() bool
}

// newScreens builds the body for every section, in rail order.
func newScreens(ctx core.Context, be backend.Backend) [core.SectionCount]screen {
	return [core.SectionCount]screen{
		core.SectionTasks:    dashboardScreen{dashboard.New(ctx, rollup.FilterAll, be)},
		core.SectionInbox:    dashboardScreen{dashboard.New(ctx, rollup.FilterAttention, be)},
		core.SectionHistory:  dashboardScreen{dashboard.New(ctx, rollup.FilterDone, be)},
		core.SectionAgents:   catalogScreen{catalog.New(ctx, catalog.Agents, be)},
		core.SectionSkills:   catalogScreen{catalog.New(ctx, catalog.Skills, be)},
		core.SectionMCPs:     catalogScreen{catalog.New(ctx, catalog.MCPs, be)},
		core.SectionSettings: catalogScreen{catalog.New(ctx, catalog.Projects, be)},
	}
}

// dashboardScreen adapts dashboard.Model, whose Update returns its concrete
// type, to the screen interface. View, Hints, Summary and Capturing are
// promoted from the embedded model.
type dashboardScreen struct{ dashboard.Model }

func (s dashboardScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	next, cmd := s.Model.Update(msg)
	return dashboardScreen{next}, cmd
}

// catalogScreen adapts catalog.Model the same way.
type catalogScreen struct{ catalog.Model }

func (s catalogScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	next, cmd := s.Model.Update(msg)
	return catalogScreen{next}, cmd
}

var (
	_ screen = dashboardScreen{}
	_ screen = catalogScreen{}
)

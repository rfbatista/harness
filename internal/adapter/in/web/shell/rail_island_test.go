package shell

import (
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

func railFrame() Frame {
	return Frame{
		Top: TopBar{Current: "p1", Projects: []Option{{ID: "p1", Name: "harness"}}},
		Rail: Rail{
			NewTaskHref: "/projects/p1/tasks/new", BoardHref: "/projects/p1", Page: PageBoard, ProductOwnerHref: "/projects/p1/product-owner",
			ReportsFeed: true, Collapsed: []string{"done"}, Current: "t1",
			Seed: &RailSeed{ProjectID: "p1", Sessions: []RailSession{{ID: "s1", TicketID: "t1", Status: "idle"}}, Tasks: []*domain.Ticket{{ID: "t1", ProjectID: "p1", Title: "Add SSE feed", Status: domain.TicketStatusInProgress}}},
		},
		Island: &TopBarIsland{Props: "{}"},
	}
}

// The rail is an island the server renders from the final frame: its mount is the
// rail's own element, its props are the seed with the navigation (the headless
// feed owner reads the same script), and the Tasks button is an island beside the
// top bar.
func TestTheRailIsAnIslandWhosePropsAreTheSeedAndTheNavigation(t *testing.T) {
	f := railFrame()
	f.RailIsland = func(f Frame) *RailIsland {
		return &RailIsland{HTML: "<b>rail</b>", Props: `{"project_id":"p1"}`, ToggleHTML: "<i>tasks</i>", ToggleProps: `{"needs_you":1}`}
	}
	got := renderShellBody(t, f)
	for _, want := range []string{
		`<div class="[ rail ]" id="rail" data-island="rail" data-current-task="t1"><b>rail</b></div>`,
		`<script type="application/json" id="rail-seed" data-island-props="rail">{"project_id":"p1"}</script>`,
		`id="rail-feed" hidden data-reports-feed="" data-seed="rail-seed" x-data="tasksRailFeed"`,
		`<a class="[ button ] [ tasks-toggle ]" data-variant="ghost" href="/projects/p1" aria-controls="rail" aria-haspopup="dialog" aria-expanded="false" data-island="tasks-toggle"><i>tasks</i></a>`,
		`<script type="application/json" data-island-props="tasks-toggle">{"needs_you":1}</script>`,
		`<div class="[ scrim ]"></div>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shell lacks %q:\n%s", want, got)
		}
	}
}

// A render that failed leaves the mounts empty and marked: the browser draws them.
func TestAFailedRailRenderLeavesTheMountsMarkedForTheBrowser(t *testing.T) {
	f := railFrame()
	f.RailIsland = func(f Frame) *RailIsland {
		return &RailIsland{Props: "{}", Failed: true, ToggleProps: "{}", ToggleFailed: true}
	}
	got := renderShellBody(t, f)
	if strings.Count(got, `data-island-rendered="false"`) < 3 { // rail, Tasks button and the product owner panel
		t.Errorf("the mounts are not marked:\n%s", got)
	}
}

// With no project the rail is the static note and no island; the pages without a rail have none.
func TestARailWithoutAProjectIsAStaticNote(t *testing.T) {
	f := Frame{Top: TopBar{Projects: []Option{{ID: "p1", Name: "harness"}}}, Island: &TopBarIsland{Props: "{}"}}
	got := renderShellBody(t, f)
	if !strings.Contains(got, `<p class="[ note ]">Choose a project in the picker above to see its tasks.</p>`) || strings.Contains(got, `data-island="rail"`) || strings.Contains(got, "tasks-toggle") {
		t.Errorf("no project:\n%s", got)
	}
	f = railFrame()
	f.NoRail = true
	f.RailIsland = func(Frame) *RailIsland { t.Fatal("a page without a rail renders none"); return nil }
	if got := renderShellBody(t, f); strings.Contains(got, `id="rail"`) || strings.Contains(got, "tasks-toggle") {
		t.Errorf("a NoRail page carries the rail:\n%s", got)
	}
}

// The rail's props are the seed's fields at the top level with the navigation, the
// folded groups and the current page beside them.
func TestTheRailPropsCarryTheSeedAtTheTopAndTheNavigation(t *testing.T) {
	p := railFrame().RailProps()
	if p.ProjectID != "p1" || p.Page != PageBoard || p.Current != "t1" || !p.ReportsFeed || len(p.Collapsed) != 1 || p.Nav.ProductOwnerHref != "/projects/p1/product-owner" || p.ProjectName != "harness" {
		t.Errorf("props = %+v", p)
	}
	if n := railFrame().Rail.Seed.NeedsYouCount(); n != 1 {
		t.Errorf("a task whose session is idle needs the person: %d", n)
	}
	if n := (*RailSeed)(nil).NeedsYouCount(); n != 0 {
		t.Errorf("no seed, no count: %d", n)
	}
}

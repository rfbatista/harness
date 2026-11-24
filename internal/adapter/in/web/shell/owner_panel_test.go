package shell

import (
	"context"
	"strings"
	"testing"
)

func renderShellBody(t *testing.T, f Frame) string {
	t.Helper()
	var b strings.Builder
	if err := shellBody(f).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A project page carries the mount of the product owner's panel, beside <main>, with its
// project in the island props; the browser fills it (ADR 0115).
func TestAProjectPageCarriesTheProductOwnerPanelMount(t *testing.T) {
	got := renderShellBody(t, Frame{Top: TopBar{Current: "p1", Projects: []Option{{ID: "p1", Name: "harness"}}}, Rail: Rail{Empty: "No tasks yet."}, Island: &TopBarIsland{HTML: "<b>bar</b>", Props: "{}"}})
	for _, want := range []string{
		`<div data-island="owner-panel" data-island-rendered="false"></div>`,
		`<script type="application/json" data-island-props="owner-panel">{"project_id":"p1","project_name":"harness","no_tasks":true}</script>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shell lacks %q:\n%s", want, got)
		}
	}
}

// A page of no project has no product owner to dock; the page without the islands built has no panel either.
func TestAPageOfNoProjectCarriesNoProductOwnerPanel(t *testing.T) {
	for name, f := range map[string]Frame{
		"no project":     {Island: &TopBarIsland{Props: "{}"}},
		"no island code": {Top: TopBar{Current: "p1"}},
	} {
		if got := renderShellBody(t, f); strings.Contains(got, "owner-panel") {
			t.Errorf("%s: the shell carries a panel mount:\n%s", name, got)
		}
	}
}

// The project id is data in a script, never markup: a hostile id cannot close the script.
func TestTheProductOwnerPanelPropsEscapeTheProjectID(t *testing.T) {
	got := renderShellBody(t, Frame{Top: TopBar{Current: `</script><b>`}, Island: &TopBarIsland{Props: "{}"}})
	if strings.Contains(got, `</script><b>`) {
		t.Errorf("project id was not escaped:\n%s", got)
	}
}

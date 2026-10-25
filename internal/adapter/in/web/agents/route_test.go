package agents

import (
	"encoding/json"
	"os"
	"testing"
)

// The page selects what the browser's domain/tabs.js selects: both read
// web/testdata/views/agents-routes.json.
func TestRoutesReadLikeTheBrowser(t *testing.T) {
	raw, err := os.ReadFile("../../../../../web/testdata/views/agents-routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Routes []struct{ Path, Tab, Item, Mode string }
		None   []string
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Routes) == 0 || len(table.None) == 0 {
		t.Fatal("the shared route table is empty")
	}
	for _, c := range table.Routes {
		got, ok := ParseRoute(c.Path)
		if want := (Route{Tab: c.Tab, Item: c.Item, Mode: c.Mode}); !ok || got != want {
			t.Errorf("ParseRoute(%q) = %+v, %v; want %+v", c.Path, got, ok, want)
		}
	}
	for _, p := range table.None {
		if got, ok := ParseRoute(p); ok {
			t.Errorf("ParseRoute(%q) = %+v; want no route", p, got)
		}
	}
}

func TestTheNewButtonFollowsTheTab(t *testing.T) {
	want := map[string][2]string{
		"agents":  {"New agent", "/agents/new"},
		"skills":  {"New skill", "/agents/skills/new"},
		"bundles": {"New bundle", "/agents/bundles/new"},
		"mcp":     {"New MCP server", "/agents/mcp/new"},
		"tools":   {"", ""},
	}
	if len(Tabs) != len(want) {
		t.Fatalf("%d tabs, want %d", len(Tabs), len(want))
	}
	for _, tab := range Tabs {
		if got := [2]string{tab.NewLabel, tab.NewHref}; got != want[tab.ID] {
			t.Errorf("%s: New = %v, want %v", tab.ID, got, want[tab.ID])
		}
	}
}

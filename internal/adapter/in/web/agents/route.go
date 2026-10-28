package agents

import (
	"net/url"
	"strings"
)

// The tabs' ids, as the browser's domain/tabs.js names them.
const (
	TabAgents  = "agents"
	TabSkills  = "skills"
	TabBundles = "bundles"
	TabMCP     = "mcp"
	TabTools   = "tools"
)

// Tab is one of the page's five tabs and the New button it gives the header.
type Tab struct {
	ID, Label, Href string
	// NewLabel and NewHref are the header's New button; "" for none.
	NewLabel, NewHref string
}

// Tabs are the page's tabs in order. The browser's TABS (domain/tabs.js)
// lists the same.
var Tabs = []Tab{
	{TabAgents, "Agents", "/agents", "New agent", "/agents/new"},
	{TabSkills, "Skills", "/agents/skills", "New skill", "/agents/skills/new"},
	{TabBundles, "Bundles", "/agents/bundles", "New bundle", "/agents/bundles/new"},
	{TabMCP, "MCP servers", "/agents/mcp", "New MCP server", "/agents/mcp/new"},
	{TabTools, "Tools", "/agents/tools", "", ""},
}

// Route is what a path shows: its tab, the item opened on it, and "new" or
// "edit" for a full-page editor.
type Route struct{ Tab, Item, Mode string }

// ParseRoute reads a path under /agents. The browser's routeOf
// (domain/tabs.js) reads the same table, web/testdata/views/agents-routes.json.
func ParseRoute(path string) (Route, bool) {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 || parts[0] != "agents" {
		return Route{}, false
	}
	rest := parts[1:]
	tab := TabAgents
	if len(rest) > 0 {
		switch rest[0] {
		case TabSkills, TabBundles, TabMCP, TabTools:
			tab, rest = rest[0], rest[1:]
		}
	}
	decoded := make([]string, len(rest))
	for i, p := range rest {
		d, err := url.PathUnescape(p)
		if err != nil {
			return Route{}, false
		}
		decoded[i] = d
	}
	switch {
	case tab == TabTools:
		return Route{Tab: tab}, len(decoded) == 0
	case len(decoded) == 0:
		return Route{Tab: tab}, true
	case len(decoded) == 1 && decoded[0] == "new":
		return Route{Tab: tab, Mode: "new"}, true
	case len(decoded) == 1:
		return Route{Tab: tab, Item: decoded[0]}, true
	case len(decoded) == 2 && decoded[1] == "edit":
		return Route{Tab: tab, Item: decoded[0], Mode: "edit"}, true
	}
	return Route{}, false
}

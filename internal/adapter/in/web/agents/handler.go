// The Agents page, /agents: the agent catalog's agents, skills, bundles, MCP
// servers and tools in five tabs. The catalog is global (every project shares
// it), so the page belongs to no project. The server paints the first view
// from the catalog read; the browser's agentsPage takes over and, when there
// is no seed, reads the catalog itself.

package agents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/shell"
)

// Href is the Agents page.
const Href = "/agents"

// Reader reads the whole catalog as GET /api/get_agent_catalog answers it.
type Reader interface {
	GetAgentCatalog(ctx context.Context) (json.RawMessage, error)
}

// Handler serves GET /agents and the pages under it.
type Handler struct {
	// Catalog seeds the page; nil, or a failed read, renders it without a
	// seed and the browser reads the catalog.
	Catalog Reader
	Layout  shell.Layout
	Render  shell.Renderer
}

// Page renders the page the path names.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	route, ok := ParseRoute(r.URL.Path)
	if !ok {
		return errs.Newf("PAGE_NOT_FOUND", "there is no page at %s", r.URL.Path)
	}
	ctx := r.Context()
	frame, err := h.Layout(ctx, "Agents", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnAgents = true
	return h.Render(w, r, http.StatusOK, Page(frame, NewView(route, h.read(r))))
}

// read is the catalog as a seed, or nil when it cannot be read.
func (h Handler) read(r *http.Request) json.RawMessage {
	if h.Catalog == nil {
		return nil
	}
	seed, err := h.Catalog.GetAgentCatalog(r.Context())
	if err != nil {
		slog.Warn("agents page: catalog not read; the browser reads it", "err", err)
		return nil
	}
	return seed
}

// View is what the page renders.
type View struct {
	Route Route
	// Seed is the catalog read, or nil when there is none.
	Seed json.RawMessage
	root string
	// counts is how many items each tab lists, by tab id; nil without a seed.
	counts map[string]int
}

// DefaultRoot names the catalog folder when the read does not.
const DefaultRoot = "<data dir>/agents/"

// NewView paints route from seed. A seed that is not a catalog is dropped.
func NewView(route Route, seed json.RawMessage) View {
	v := View{Route: route}
	var c struct {
		Root       string            `json:"root"`
		Agents     []json.RawMessage `json:"agents"`
		Bundles    []json.RawMessage `json:"bundles"`
		Skills     []json.RawMessage `json:"skills"`
		MCPServers []json.RawMessage `json:"mcp_servers"`
		Tools      []json.RawMessage `json:"built_in_tools"`
	}
	if len(seed) == 0 || json.Unmarshal(seed, &c) != nil {
		return v
	}
	v.Seed, v.root = seed, c.Root
	v.counts = map[string]int{TabAgents: len(c.Agents), TabSkills: len(c.Skills), TabBundles: len(c.Bundles), TabMCP: len(c.MCPServers), TabTools: len(c.Tools)}
	return v
}

// Root is the catalog folder the page names.
func (v View) Root() string {
	if v.root == "" {
		return DefaultRoot
	}
	return v.root
}

// Count is how many items tab lists, or "" before the catalog is known.
func (v View) Count(tab string) string {
	n, ok := v.counts[tab]
	if !ok {
		return ""
	}
	return strconv.Itoa(n)
}

// Editing is whether the route is an agent editor, a full page that takes the
// place of the tabs.
func (v View) Editing() bool { return v.Route.Tab == TabAgents && v.Route.Mode != "" }

// Selected is whether tab is the one the route names.
func (v View) Selected(tab string) bool { return v.Route.Tab == tab }

// Current is the tab the route names.
func (v View) Current() Tab {
	for _, t := range Tabs {
		if t.ID == v.Route.Tab {
			return t
		}
	}
	return Tabs[0]
}

// ItemNote says which item a deep link opens, until the tab lists its detail.
func (v View) ItemNote() string {
	switch {
	case v.Route.Item == "":
		return ""
	case v.Route.Mode == "new":
		return "New " + v.Route.Item
	case v.Route.Mode == "edit":
		return "Edit " + v.Route.Item
	}
	return v.Route.Item
}

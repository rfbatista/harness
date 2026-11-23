package web

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"operators-mcp/internal/adapter/in/web/agentisolation"
	"operators-mcp/internal/adapter/in/web/agents"
	"operators-mcp/internal/adapter/in/web/credentials"
	"operators-mcp/internal/adapter/in/web/home"
	"operators-mcp/internal/adapter/in/web/models"
	"operators-mcp/internal/adapter/in/web/notifications"
	"operators-mcp/internal/adapter/in/web/projects"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/in/web/tokensavers"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// switchProjectHref is the picker's GET form target.
const switchProjectHref = "/switch-project"

// NewTopBars builds the top bar's model from what the pages know, for
// GET /api/top_bar. Nothing is held between calls.
func NewTopBars(deps Deps) ports.TopBars {
	return &server{deps: deps}
}

// TopBar is the top bar's model for a page path, read fresh: the one builder
// behind both the pages' island props and the API.
func (s *server) TopBar(ctx context.Context, path string) (*ports.TopBar, error) {
	all, err := s.deps.Projects.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b *domain.Project) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	bar := &ports.TopBar{
		Path:              path,
		Projects:          make([]ports.TopBarProject, 0, len(all)),
		SwitchProjectHref: switchProjectHref,
		Links:             s.topBarLinks(path),
	}
	id := projectOfPath(path)
	for _, p := range all {
		bar.Projects = append(bar.Projects, ports.TopBarProject{ID: p.ID, Name: p.Name})
		if p.ID == id {
			bar.CurrentProjectID = id
		}
	}
	bar.Notifications = s.topBarBell(ctx)
	return bar, nil
}

// topBarLinks names the pages this server serves, in the bar's order, with the
// one the path opens marked current and the attention mark where agents are
// blocked.
func (s *server) topBarLinks(path string) []ports.TopBarLink {
	var links []ports.TopBarLink
	add := func(key, href string, current bool) *ports.TopBarLink {
		links = append(links, ports.TopBarLink{Key: key, Href: href, Current: current})
		return &links[len(links)-1]
	}
	if s.deps.AgentBoard != nil {
		add("board", home.BoardHref, path == home.BoardHref)
	}
	if s.deps.ProjectSummaries != nil {
		add("projects", projects.ListHref, path == projects.ListHref)
	} else {
		add("new_project", "/projects/new", false)
	}
	add("agents", agents.Href, path == agents.Href || strings.HasPrefix(path, agents.Href+"/"))
	if s.deps.GitCredentials != nil {
		add("credentials", credentials.Href, path == credentials.Href)
	}
	if s.deps.TokenSaverInstalls != nil {
		add("token_savers", tokensavers.Href, path == tokensavers.Href)
	}
	if s.deps.Settings != nil {
		add("models", models.Href, path == models.Href)
	}
	if s.deps.SandboxSettings != nil {
		l := add("agent_isolation", agentisolation.Href, path == agentisolation.Href)
		l.Attention = agentisolation.Blocks(s.deps.SandboxSettings, s.deps.SandboxDriver, s.deps.AgentCredential, s.deps.AgentLogin, s.deps.ManagedClaudeBin)
	}
	if s.deps.DataPaths != nil {
		add("server", serversettings.Href, path == serversettings.Href)
	}
	return links
}

// topBarBell is the way to the inbox with its first page; nil when the inbox is
// not served. A failed read still gives the bell: the client reads the page.
func (s *server) topBarBell(ctx context.Context) *ports.TopBarBell {
	if s.deps.Notifications == nil {
		return nil
	}
	bell := &ports.TopBarBell{InboxHref: notifications.Href}
	page, err := s.deps.Notifications.ListNotifications(ctx, ports.NotificationQuery{Limit: notifications.PanelLimit})
	if err != nil {
		slog.Warn("web: top bar notifications not read; the browser reads them", "err", err)
		return bell
	}
	bell.Page = page
	return bell
}

// projectOfPath is the project a page path names, "" for a page that belongs to
// none (/projects/new included).
func projectOfPath(path string) string {
	rest, ok := strings.CutPrefix(path, "/projects/")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, "/")
	id, err := url.PathUnescape(id)
	if err != nil || id == "new" {
		return ""
	}
	return id
}

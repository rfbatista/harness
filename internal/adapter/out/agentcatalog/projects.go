package agentcatalog

import (
	"context"

	"operators-mcp/internal/domain"
)

func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listProjects is every project, or none when the catalog has no projects.
func (c *Catalog) listProjects() []*domain.Project {
	if c.projects == nil {
		return nil
	}
	list, err := c.projects.ListProjects(context.Background())
	if err != nil {
		return nil
	}
	return list
}

// projectNames are a project's MCP server names now.
func (c *Catalog) projectNames(id string) ([]string, bool) {
	for _, p := range c.listProjects() {
		if p.ID == id {
			return p.MCPServers, true
		}
	}
	return nil, false
}

func (c *Catalog) setProjectNames(id string, names []string) error {
	if c.projects == nil {
		return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + id + " not found"}
	}
	_, err := c.projects.SetProjectMCPServers(context.Background(), id, names)
	return err
}

// projectLists is every project, read once per write.
func (t *txn) projectLists() []*domain.Project {
	if t.projectList == nil {
		t.projectList = t.c.listProjects()
		if t.projectList == nil {
			t.projectList = []*domain.Project{}
		}
	}
	return t.projectList
}

// currentProjectNames are a project's list with this write's edit applied.
func (t *txn) currentProjectNames(p *domain.Project) []string {
	for _, e := range t.projects {
		if e.id == p.ID {
			return e.after
		}
	}
	return p.MCPServers
}

// setProject records a project's new list. The write's own earlier edit of
// the same project is replaced.
func (t *txn) setProject(p *domain.Project, names []string) {
	for _, e := range t.projects {
		if e.id == p.ID {
			e.after = names
			return
		}
	}
	e := t.editOf(p)
	e.after = names
}

// editOf is the write's edit of a project, made when there is none.
func (t *txn) editOf(p *domain.Project) *projectEdit {
	for _, e := range t.projects {
		if e.id == p.ID {
			return e
		}
	}
	e := &projectEdit{id: p.ID, name: p.Name,
		before: append([]string{}, p.MCPServers...), after: append([]string{}, p.MCPServers...),
		enBefore: append([]string{}, p.EnabledAgents...), enAfter: append([]string{}, p.EnabledAgents...),
		banBefore: append([]string{}, p.MCPBanned...), banAfter: append([]string{}, p.MCPBanned...),
		ovBefore: copyOverrides(p.MCPToolOverrides), ovAfter: copyOverrides(p.MCPToolOverrides)}
	t.projects = append(t.projects, e)
	return e
}

// enabledAgents are a selected project's agents with this write's edit applied.
func (t *txn) enabledAgents(p *domain.Project) []string {
	return t.editOf(p).enAfter
}

// setEnabledAgents records a selected project's new agent list.
func (t *txn) setEnabledAgents(p *domain.Project, names []string) {
	t.editOf(p).enAfter = names
}

// projectOf is the project now, or nil.
func (c *Catalog) projectOf(id string) *domain.Project {
	for _, p := range c.listProjects() {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// applyProject writes a project edit's lists, or puts the old ones back.
func (c *Catalog) applyProject(p *projectEdit, forward bool) error {
	mcp, en, prevMCP, prevEn := p.after, p.enAfter, p.before, p.enBefore
	if !forward {
		mcp, en, prevMCP, prevEn = p.before, p.enBefore, p.after, p.enAfter
	}
	if !sameNames(mcp, prevMCP) {
		if err := c.setProjectNames(p.id, mcp); err != nil {
			return err
		}
	}
	if p.settingsChanged() {
		ban, ov := p.banAfter, p.ovAfter
		if !forward {
			ban, ov = p.banBefore, p.ovBefore
		}
		if _, err := c.projects.SetProjectMCPSettings(context.Background(), p.id, domain.ProjectMCPSettings{Banned: ban, ToolOverrides: ov}); err != nil {
			return err
		}
	}
	if !sameNames(en, prevEn) {
		if _, err := c.projects.SetProjectAgents(context.Background(), p.id, domain.AgentsSelected, en); err != nil {
			return err
		}
	}
	return nil
}

// dropUnchangedProjects forgets the edits that end where they began.
func (t *txn) dropUnchangedProjects() {
	kept := t.projects[:0]
	for _, e := range t.projects {
		if e.listChanged() || e.settingsChanged() {
			kept = append(kept, e)
		}
	}
	t.projects = kept
}

func copyOverrides(in map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for server, tools := range in {
		out[server] = make(map[string]string, len(tools))
		for tool, v := range tools {
			out[server][tool] = v
		}
	}
	return out
}

// moveProjectMCPSettings makes every project's ban and tool overrides of the
// server from follow its rename to to, or go with its delete when to is "".
func (t *txn) moveProjectMCPSettings(from, to string) {
	for _, p := range t.projectLists() {
		if !contains(p.MCPBanned, from) && p.MCPToolOverrides[from] == nil {
			continue
		}
		e := t.editOf(p)
		if contains(e.banAfter, from) {
			if to == "" {
				e.banAfter = without(e.banAfter, from)
			} else {
				e.banAfter = replaceName(e.banAfter, from, to)
			}
		}
		if tools, ok := e.ovAfter[from]; ok {
			delete(e.ovAfter, from)
			if to != "" {
				e.ovAfter[to] = tools
			}
		}
	}
}

// projectMCPView is a project's MCP settings: every catalog server with the
// state the project gave it, who lists it and its tools in effect.
func (c *Catalog) projectMCPView(p *domain.Project) (*domain.CatalogProjectMCPServers, error) {
	cat, err := c.ReadCatalog()
	if err != nil {
		return nil, err
	}
	out := &domain.CatalogProjectMCPServers{MCPServers: nonNil(p.MCPServers), Servers: []domain.CatalogProjectMCPServer{}}
	for _, s := range cat.MCPServers {
		srv := domain.CatalogProjectMCPServer{Name: s.Name, State: domain.MCPStateAgent, UsedByAgents: []string{}}
		switch {
		case contains(p.MCPBanned, s.Name):
			srv.State = domain.MCPStateBanned
		case contains(p.MCPServers, s.Name):
			srv.State = domain.MCPStateAdded
		}
		for _, a := range cat.Agents {
			if contains(a.MCPServers, s.Name) {
				srv.UsedByAgents = append(srv.UsedByAgents, a.Name)
			}
		}
		if s.KnownTools != nil {
			srv.Tools = make([]domain.CatalogProjectMCPTool, 0, len(s.KnownTools))
			for _, name := range s.KnownTools {
				tool := domain.CatalogProjectMCPTool{Name: name, Default: domain.ToolEnabled, Override: "default"}
				if contains(s.DisabledTools, name) {
					tool.Default = domain.ToolDisabled
				}
				tool.Effective = tool.Default
				if o := p.MCPToolOverrides[s.Name][name]; o == domain.ToolEnabled || o == domain.ToolDisabled {
					tool.Override, tool.Effective = o, o
				}
				srv.Tools = append(srv.Tools, tool)
			}
		}
		out.Servers = append(out.Servers, srv)
	}
	return out, nil
}

// GetProjectMCPServers reads the MCP settings of a project.
func (c *Catalog) GetProjectMCPServers(_ context.Context, projectID string) (*domain.CatalogProjectMCPServers, error) {
	p := c.projectOf(projectID)
	if p == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + projectID + " not found"}
	}
	return c.projectMCPView(p)
}

// SetProjectMCPServers makes the named servers Added. An Added server that is
// not named goes back to Agent's choice; bans and tool overrides stay, except
// that naming a banned server adds it. Each name must be in the catalog.
func (c *Catalog) SetProjectMCPServers(_ context.Context, in domain.SetCatalogProjectMCPServers) (*domain.CatalogProjectMCPServers, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	p := c.projectOf(in.ProjectID)
	if p == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + in.ProjectID + " not found"}
	}
	l, err := c.load()
	if err != nil {
		return nil, err
	}
	var unknown []string
	seen := map[string]bool{}
	names := []string{}
	for _, n := range in.MCPServers {
		if n != domain.ServicesServerName && l.servers.get(n) == nil {
			unknown = append(unknown, n)
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(unknown) > 0 {
		return nil, unknownRefs(unknown)
	}
	if err := c.setProjectNames(in.ProjectID, names); err != nil {
		return nil, err
	}
	banned := []string{}
	for _, b := range p.MCPBanned {
		if !seen[b] {
			banned = append(banned, b)
		}
	}
	if len(banned) != len(p.MCPBanned) {
		if _, err := c.projects.SetProjectMCPSettings(context.Background(), in.ProjectID, domain.ProjectMCPSettings{Banned: banned, ToolOverrides: p.MCPToolOverrides}); err != nil {
			return nil, err
		}
	}
	c.last = nil // the latest change is no longer the latest write
	c.publish([]string{"projects/" + in.ProjectID})
	return c.projectMCPView(c.projectOf(in.ProjectID))
}

// SetProjectMCPSettings replaces a project's states and tool overrides: a
// catalog server not named is Agent's choice with no overrides.
func (c *Catalog) SetProjectMCPSettings(_ context.Context, in domain.SetCatalogProjectMCPSettings) (*domain.CatalogProjectMCPServers, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.projectOf(in.ProjectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + in.ProjectID + " not found"}
	}
	l, err := c.load()
	if err != nil {
		return nil, err
	}
	var unknown, added, banned []string
	seen := map[string]bool{}
	overrides := map[string]map[string]string{}
	for _, s := range in.Servers {
		if seen[s.Name] {
			return nil, invalid("servers", "the server "+s.Name+" is named twice")
		}
		seen[s.Name] = true
		switch s.State {
		case domain.MCPStateAdded:
			added = append(added, s.Name)
		case domain.MCPStateBanned:
			banned = append(banned, s.Name)
		case domain.MCPStateAgent:
		default:
			return nil, invalid("state", "state is added, agent or banned")
		}
		for tool, v := range s.ToolOverrides {
			if v != domain.ToolEnabled && v != domain.ToolDisabled {
				return nil, invalid("tool_overrides", "an override is enabled or disabled")
			}
			if overrides[s.Name] == nil {
				overrides[s.Name] = map[string]string{}
			}
			overrides[s.Name][tool] = v
		}
		if s.Name != domain.ServicesServerName && l.servers.get(s.Name) == nil {
			unknown = append(unknown, s.Name)
		}
	}
	if len(unknown) > 0 {
		return nil, unknownRefs(unknown)
	}
	if added == nil {
		added = []string{}
	}
	if err := c.setProjectNames(in.ProjectID, added); err != nil {
		return nil, err
	}
	if _, err := c.projects.SetProjectMCPSettings(context.Background(), in.ProjectID, domain.ProjectMCPSettings{Banned: banned, ToolOverrides: overrides}); err != nil {
		return nil, err
	}
	c.last = nil
	c.publish([]string{"projects/" + in.ProjectID})
	return c.projectMCPView(c.projectOf(in.ProjectID))
}

// GetProjectAgents reads a project's enabled agents.
func (c *Catalog) GetProjectAgents(_ context.Context, projectID string) (*domain.CatalogProjectAgents, error) {
	for _, p := range c.listProjects() {
		if p.ID == projectID {
			if p.EnabledAgentsMode != domain.AgentsSelected {
				return &domain.CatalogProjectAgents{Mode: domain.AgentsAll, Agents: []string{}}, nil
			}
			return &domain.CatalogProjectAgents{Mode: domain.AgentsSelected, Agents: nonNil(p.EnabledAgents)}, nil
		}
	}
	return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + projectID + " not found"}
}

// SetProjectAgents replaces a project's enabled agents. Each name must be a
// catalog agent. In all mode the list is ignored and stored empty.
func (c *Catalog) SetProjectAgents(ctx context.Context, in domain.SetCatalogProjectAgents) (*domain.CatalogProjectAgents, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.GetProjectAgents(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	var names []string
	switch in.Mode {
	case domain.AgentsAll:
		names = []string{}
	case domain.AgentsSelected:
		l, err := c.load()
		if err != nil {
			return nil, err
		}
		var unknown []string
		seen := map[string]bool{}
		names = []string{}
		for _, n := range in.Agents {
			if l.agents.get("agents").get(n) == nil {
				unknown = append(unknown, n)
			}
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		if len(unknown) > 0 {
			return nil, unknownRefs(unknown)
		}
	default:
		return nil, invalid("mode", "mode is all or selected")
	}
	if _, err := c.projects.SetProjectAgents(ctx, in.ProjectID, in.Mode, names); err != nil {
		return nil, err
	}
	c.last = nil
	c.publish([]string{"projects/" + in.ProjectID})
	return &domain.CatalogProjectAgents{Mode: in.Mode, Agents: names}, nil
}

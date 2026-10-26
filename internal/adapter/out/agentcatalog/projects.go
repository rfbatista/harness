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
		enBefore: append([]string{}, p.EnabledAgents...), enAfter: append([]string{}, p.EnabledAgents...)}
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
		if !sameNames(e.before, e.after) || !sameNames(e.enBefore, e.enAfter) {
			kept = append(kept, e)
		}
	}
	t.projects = kept
}

// GetProjectMCPServers reads the MCP servers a project gives its sessions.
func (c *Catalog) GetProjectMCPServers(_ context.Context, projectID string) (*domain.CatalogProjectMCPServers, error) {
	names, ok := c.projectNames(projectID)
	if !ok {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project " + projectID + " not found"}
	}
	return &domain.CatalogProjectMCPServers{MCPServers: nonNil(names)}, nil
}

// SetProjectMCPServers replaces the MCP servers a project gives its sessions.
// Each name must be in the catalog.
func (c *Catalog) SetProjectMCPServers(_ context.Context, in domain.SetCatalogProjectMCPServers) (*domain.CatalogProjectMCPServers, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, ok := c.projectNames(in.ProjectID); !ok {
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
		if l.servers.get(n) == nil {
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
	c.last = nil // the latest change is no longer the latest write
	c.publish([]string{"projects/" + in.ProjectID})
	return &domain.CatalogProjectMCPServers{MCPServers: names}, nil
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

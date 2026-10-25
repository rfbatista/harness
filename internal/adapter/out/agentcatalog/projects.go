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
	t.projects = append(t.projects, &projectEdit{id: p.ID, name: p.Name, before: append([]string{}, p.MCPServers...), after: names})
}

// dropUnchangedProjects forgets the edits that end where they began.
func (t *txn) dropUnchangedProjects() {
	kept := t.projects[:0]
	for _, e := range t.projects {
		if !sameNames(e.before, e.after) {
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
	c.publish([]string{"projects/" + in.ProjectID})
	return &domain.CatalogProjectMCPServers{MCPServers: names}, nil
}

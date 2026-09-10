package backend

import (
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Snapshot is one consistent read of everything the list screens show. The
// root model reloads it on a timer and hands it to every screen, so screens
// never read the backend themselves.
type Snapshot struct {
	Projects     []*domain.Project
	Repositories []*domain.Repository
	Agents       []*domain.Agent
	Skills       []*domain.Skill
	MCPServers   []*domain.MCPServer
	Tickets      []*domain.Ticket
	Sessions     []*domain.Session
	Settings     map[string]string
}

// Load reads every catalog once. Settings errors are swallowed into an empty
// map: a missing settings row must not blank the dashboard.
func Load(b Backend) Snapshot {
	s := Snapshot{
		Projects:   b.ListProjects(),
		Agents:     b.ListAgents(),
		Skills:     b.ListSkills(),
		MCPServers: b.ListMCPServers(),
		Tickets:    b.ListTickets(""),
		Sessions:   b.ListSessions(ports.SessionFilter{}),
	}
	for _, p := range s.Projects {
		s.Repositories = append(s.Repositories, b.ListRepositories(p.ID)...)
	}
	settings, err := b.GetSettings()
	if err != nil || settings == nil {
		settings = map[string]string{}
	}
	s.Settings = settings
	return s
}

// Project finds a project by id, or nil.
func (s Snapshot) Project(id string) *domain.Project {
	for _, p := range s.Projects {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// ProjectName is the display name for a project id; unknown ids fall back to
// the id so a row never loses its project column.
func (s Snapshot) ProjectName(id string) string {
	if p := s.Project(id); p != nil {
		return p.Name
	}
	return id
}

// RepositoriesOf lists the repositories linked to one project.
func (s Snapshot) RepositoriesOf(projectID string) []*domain.Repository {
	var out []*domain.Repository
	for _, r := range s.Repositories {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out
}

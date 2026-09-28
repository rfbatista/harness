package blueprint

import (
	"github.com/rfbatista/harnesskit/mcpprobe"
	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"
	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service satisfies every driving port of this package, checked at compile time.
var _ ports.Blueprint = (*Service)(nil)

// Service implements blueprint use cases by delegating to the outbound ports.
// It is the application (use-case) layer in hexagonal architecture.
type Service struct {
	Projects     ports.ProjectRepository
	Repositories ports.RepositoryRepository
	Zones        ports.ZoneRepository
	Agents       ports.AgentRepository
	Prompts      ports.PromptRepository
	Skills       ports.SkillRepository
	MCPServers   ports.MCPServerRepository
	Tools        ports.ToolRepository
	PathMatcher  ports.PathMatcher
	TreeLister   ports.TreeLister
	DefaultRoot  string
	Settings     ports.SettingsRepository
	Publisher    ports.SkillPublisher

	// skillSvc owns the skill use cases. It is built in NewService and
	// mutated by WithPublishing, never rebuilt: two instances would diverge.
	skillSvc *skill.Service
	// mcpSvc owns the MCP server use cases.
	mcpSvc *mcpserver.Service

	BoundedContexts ports.BoundedContextRepository
}

// NewService returns a blueprint application service with the given ports.
// defaultRoot is used when no project_id is provided for list_tree/list_matching_paths.
func NewService(projects ports.ProjectRepository, repositories ports.RepositoryRepository, zones ports.ZoneRepository, agents ports.AgentRepository, prompts ports.PromptRepository, skills ports.SkillRepository, mcpServers ports.MCPServerRepository, tools ports.ToolRepository, pathMatcher ports.PathMatcher, treeLister ports.TreeLister, defaultRoot string) *Service {
	svc := &Service{
		Projects:     projects,
		Repositories: repositories,
		Zones:        zones,
		Agents:       agents,
		Prompts:      prompts,
		Skills:       skills,
		MCPServers:   mcpServers,
		Tools:        tools,
		PathMatcher:  pathMatcher,
		TreeLister:   treeLister,
		DefaultRoot:  defaultRoot,
	}
	svc.skillSvc = skill.NewService(skills).WithHooks(skill.Hooks{
		BeforeDeleteRow: svc.unlinkSkillFromAgents,
	})
	svc.mcpSvc = mcpserver.NewService(mcpServers).
		WithProber(mcpprobe.NewProber()).
		WithHooks(mcpserver.Hooks{
			Decorate:     svc.enrichMCPServerUsage,
			BeforeDelete: svc.unlinkMCPServerFromAgents,
		})
	return svc
}

// WithPublishing attaches the settings repository and skill publisher that back
// the publish/unpublish use cases. Without them publishing is unavailable and
// skill mutations never touch the filesystem.
func (s *Service) WithPublishing(settings ports.SettingsRepository, publisher ports.SkillPublisher) *Service {
	s.Settings = settings
	s.Publisher = publisher
	s.skillSvc.WithPublishing(s.publishRoot, publisher)
	return s
}

// WithBoundedContexts attaches the bounded context repository that backs the
// bounded context use cases. Without it those use cases are unavailable.
func (s *Service) WithBoundedContexts(boundedContexts ports.BoundedContextRepository) *Service {
	s.BoundedContexts = boundedContexts
	return s
}

// resolveRoot returns the root path for tree/path operations. If root is non-empty it is used;
// else if projectID is non-empty the project's RootDir is used; otherwise DefaultRoot.
func (s *Service) resolveRoot(root, projectID string) (string, error) {
	if root != "" {
		return root, nil
	}
	if projectID != "" {
		p := s.Projects.Get(projectID)
		if p == nil {
			return "", &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
		}
		return p.RootDir, nil
	}
	return s.DefaultRoot, nil
}

// ResolveAgentPrompt populates the Prompt field on an agent by looking up PromptID.
func (s *Service) ResolveAgentPrompt(a *domain.Agent) {
	if a == nil || a.PromptID == "" || s.Prompts == nil {
		return
	}
	a.Prompt = s.Prompts.Get(a.PromptID)
}

// ResolveAgentSkills populates the Skills field on an agent by looking up SkillIDs.
func (s *Service) ResolveAgentSkills(a *domain.Agent) {
	if a == nil {
		return
	}
	a.Skills = nil
	if len(a.SkillIDs) == 0 || s.Skills == nil {
		return
	}
	for _, id := range a.SkillIDs {
		if skill := s.Skills.Get(id); skill != nil {
			hydrated := s.hydrateSkill(skill)
			a.Skills = append(a.Skills, *hydrated)
		}
	}
}

// ResolveAgentMCPServers populates the MCPServers field on an agent by looking up MCPServerIDs.
func (s *Service) ResolveAgentMCPServers(a *domain.Agent) {
	if a == nil {
		return
	}
	a.MCPServers = nil
	if len(a.MCPServerIDs) == 0 || s.MCPServers == nil {
		return
	}
	for _, id := range a.MCPServerIDs {
		if mcpServer := s.MCPServers.Get(id); mcpServer != nil {
			a.MCPServers = append(a.MCPServers, *mcpServer)
		}
	}
}

// ResolveAgentRelations populates read-time prompt, skill, and MCP server data for an agent.
func (s *Service) ResolveAgentRelations(a *domain.Agent) {
	s.ResolveAgentPrompt(a)
	s.ResolveAgentSkills(a)
	s.ResolveAgentMCPServers(a)
}

// ListProjects returns all projects.
func (s *Service) ListProjects() []*domain.Project {
	return s.Projects.List()
}

// GetProject returns one project by id, or nil if not found.
func (s *Service) GetProject(projectID string) *domain.Project {
	return s.Projects.Get(projectID)
}

// CreateProject creates a project with the given name and root directory.
func (s *Service) CreateProject(name, rootDir string) (*domain.Project, error) {
	return s.Projects.Create(name, rootDir)
}

// UpdateProject updates an existing project.
func (s *Service) UpdateProject(projectID, name, rootDir string) (*domain.Project, error) {
	return s.Projects.Update(projectID, name, rootDir)
}

// DeleteProject deletes a project and all its zones.
func (s *Service) DeleteProject(projectID string) error {
	if s.Projects.Get(projectID) == nil {
		return &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	if err := s.Zones.DeleteByProject(projectID); err != nil {
		return err
	}
	if s.Repositories != nil {
		if err := s.Repositories.DeleteByProject(projectID); err != nil {
			return err
		}
	}
	if s.BoundedContexts != nil {
		if err := s.BoundedContexts.DeleteByProject(projectID); err != nil {
			return err
		}
	}
	return s.Projects.Delete(projectID)
}

// AddIgnoredPath adds a path to the project's ignored list (hidden in tree view).
func (s *Service) AddIgnoredPath(projectID, path string) (*domain.Project, error) {
	return s.Projects.AddIgnoredPath(projectID, path)
}

// RemoveIgnoredPath removes a path from the project's ignored list.
func (s *Service) RemoveIgnoredPath(projectID, path string) (*domain.Project, error) {
	return s.Projects.RemoveIgnoredPath(projectID, path)
}

// --- Repository CRUD ---

// ListRepositories returns all git repositories for the given project.
func (s *Service) ListRepositories(projectID string) []*domain.Repository {
	if s.Repositories == nil {
		return nil
	}
	return s.Repositories.ListByProject(projectID)
}

// GetRepository returns one repository by id, or nil if not found.
func (s *Service) GetRepository(id string) *domain.Repository {
	if s.Repositories == nil {
		return nil
	}
	return s.Repositories.Get(id)
}

// CreateRepository creates a git repository scoped to a project.
func (s *Service) CreateRepository(projectID, name, description, url, rootDir string) (*domain.Repository, error) {
	if s.Repositories == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}
	}
	if s.Projects.Get(projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	return s.Repositories.Create(projectID, name, description, url, rootDir)
}

// UpdateRepository updates an existing git repository.
func (s *Service) UpdateRepository(id, name, description, url, rootDir string) (*domain.Repository, error) {
	if s.Repositories == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}
	}
	return s.Repositories.Update(id, name, description, url, rootDir)
}

// DeleteRepository deletes a git repository by id.
func (s *Service) DeleteRepository(id string) error {
	if s.Repositories == nil {
		return &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}
	}
	return s.Repositories.Delete(id)
}

// AddRepositoryIgnoredPath adds a path to a repository's ignored list.
func (s *Service) AddRepositoryIgnoredPath(repositoryID, path string) (*domain.Repository, error) {
	if s.Repositories == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}
	}
	return s.Repositories.AddIgnoredPath(repositoryID, path)
}

// RemoveRepositoryIgnoredPath removes a path from a repository's ignored list.
func (s *Service) RemoveRepositoryIgnoredPath(repositoryID, path string) (*domain.Repository, error) {
	if s.Repositories == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "repository store not configured"}
	}
	return s.Repositories.RemoveIgnoredPath(repositoryID, path)
}

// --- Bounded context CRUD ---

// ListBoundedContexts returns all bounded contexts for the given project.
func (s *Service) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	if s.BoundedContexts == nil {
		return nil
	}
	return s.BoundedContexts.ListByProject(projectID)
}

// GetBoundedContext returns one bounded context by id, or nil if not found.
func (s *Service) GetBoundedContext(id string) *domain.BoundedContext {
	if s.BoundedContexts == nil {
		return nil
	}
	return s.BoundedContexts.Get(id)
}

// CreateBoundedContext creates a bounded context scoped to a project.
func (s *Service) CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if s.BoundedContexts == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "bounded context store not configured"}
	}
	if s.Projects.Get(projectID) == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}
	return s.BoundedContexts.Create(projectID, name, purpose, terms)
}

// UpdateBoundedContext updates an existing bounded context; the ubiquitous
// language is replaced wholesale.
func (s *Service) UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if s.BoundedContexts == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "bounded context store not configured"}
	}
	return s.BoundedContexts.Update(id, name, purpose, terms)
}

// DeleteBoundedContext deletes a bounded context. Zones linked to it are
// unlinked, not deleted.
func (s *Service) DeleteBoundedContext(id string) error {
	if s.BoundedContexts == nil {
		return &domain.StructuredError{Code: "INTERNAL", Message: "bounded context store not configured"}
	}
	if s.BoundedContexts.Get(id) == nil {
		return &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
	}
	if err := s.Zones.ClearBoundedContext(id); err != nil {
		return err
	}
	return s.BoundedContexts.Delete(id)
}

// AssignZoneToBoundedContext links a zone to a bounded context of the same project.
func (s *Service) AssignZoneToBoundedContext(zoneID, boundedContextID string) (*domain.Zone, error) {
	if s.BoundedContexts == nil {
		return nil, &domain.StructuredError{Code: "INTERNAL", Message: "bounded context store not configured"}
	}
	bc := s.BoundedContexts.Get(boundedContextID)
	if bc == nil {
		return nil, &domain.StructuredError{Code: "BOUNDED_CONTEXT_NOT_FOUND", Message: "bounded context not found"}
	}
	zone := s.Zones.Get(zoneID)
	if zone == nil {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}
	if zone.ProjectID != bc.ProjectID {
		return nil, &domain.StructuredError{Code: "CROSS_PROJECT_ACCESS", Message: "zone and bounded context belong to different projects"}
	}
	return s.Zones.SetBoundedContext(zoneID, boundedContextID)
}

// UnassignZoneFromBoundedContext clears a zone's bounded context link.
func (s *Service) UnassignZoneFromBoundedContext(zoneID string) (*domain.Zone, error) {
	return s.Zones.SetBoundedContext(zoneID, "")
}

// ListMatchingPaths returns paths under root that match the regex pattern.
// root and projectID are optional; if both empty, DefaultRoot is used.
func (s *Service) ListMatchingPaths(root, projectID, pattern string) ([]string, error) {
	r, err := s.resolveRoot(root, projectID)
	if err != nil {
		return nil, err
	}
	return s.PathMatcher.ListMatchingPaths(r, pattern)
}

// ListTree returns the directory tree from root.
// root and projectID are optional; if both empty, DefaultRoot is used.
func (s *Service) ListTree(root, projectID string) (*domain.TreeNode, error) {
	r, err := s.resolveRoot(root, projectID)
	if err != nil {
		return nil, err
	}
	return s.TreeLister.ListTree(r)
}

// ListZones returns all zones for the given project with resolved rules.
func (s *Service) ListZones(projectID string) []*domain.Zone {
	zones := s.Zones.ListByProject(projectID)
	for _, z := range zones {
		s.resolveZoneRules(z)
	}
	return zones
}

// GetZone returns one zone by id with resolved rules, or nil if not found.
func (s *Service) GetZone(zoneID string) *domain.Zone {
	z := s.Zones.Get(zoneID)
	s.resolveZoneRules(z)
	return z
}

// CreateZone creates a zone in the given project with the given metadata.
func (s *Service) CreateZone(projectID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	z, err := s.Zones.Create(projectID, name, pattern, purpose, rules, agents)
	if err != nil {
		return nil, err
	}
	s.resolveZoneRules(z)
	return z, nil
}

// UpdateZone updates an existing zone.
func (s *Service) UpdateZone(zoneID, name, pattern, purpose string, rules []domain.Prompt, agents []domain.Agent) (*domain.Zone, error) {
	z, err := s.Zones.Update(zoneID, name, pattern, purpose, rules, agents)
	if err != nil {
		return nil, err
	}
	s.resolveZoneRules(z)
	return z, nil
}

// resolveZoneRules hydrates rule Prompt objects from their IDs.
func (s *Service) resolveZoneRules(z *domain.Zone) {
	if z == nil {
		return
	}
	for i, r := range z.Rules {
		if p := s.Prompts.Get(r.ID); p != nil {
			z.Rules[i] = *p
		}
	}
}

// AssignPathToZone adds a path to a zone's explicit paths (path is normalized).
func (s *Service) AssignPathToZone(zoneID, path string) (*domain.Zone, error) {
	return s.Zones.AssignPath(zoneID, domain.NormalizePath(path))
}

// UnassignPathFromZone removes a path from a zone's explicit paths (path is normalized). No-op if path was not in explicit paths.
func (s *Service) UnassignPathFromZone(zoneID, path string) (*domain.Zone, error) {
	return s.Zones.UnassignPath(zoneID, domain.NormalizePath(path))
}

// --- Prompt CRUD ---

// ListPrompts returns all prompts.
func (s *Service) ListPrompts() []*domain.Prompt {
	return s.Prompts.List()
}

// GetPrompt returns one prompt by id, or nil if not found.
func (s *Service) GetPrompt(id string) *domain.Prompt {
	return s.Prompts.Get(id)
}

// CreatePrompt creates a prompt with the given name, description, and content.
func (s *Service) CreatePrompt(name, description, content string) (*domain.Prompt, error) {
	return s.Prompts.Create(name, description, content)
}

// UpdatePrompt updates an existing prompt.
func (s *Service) UpdatePrompt(id, name, description, content string) (*domain.Prompt, error) {
	return s.Prompts.Update(id, name, description, content)
}

// DeletePrompt deletes a prompt, unlinks it from all agents, and removes it from all zone rules.
func (s *Service) DeletePrompt(id string) error {
	if s.Prompts.Get(id) == nil {
		return &domain.StructuredError{Code: "PROMPT_NOT_FOUND", Message: "prompt not found"}
	}
	for _, a := range s.Agents.List() {
		if a.PromptID == id {
			if _, err := s.Agents.Update(a.ID, a.Name, a.Description, "", a.SkillIDs, a.MCPServerIDs); err != nil {
				return err
			}
		}
	}
	for _, p := range s.Projects.List() {
		for _, z := range s.Zones.ListByProject(p.ID) {
			var hasRule bool
			for _, r := range z.Rules {
				if r.ID == id {
					hasRule = true
					break
				}
			}
			if hasRule {
				filtered := make([]domain.Prompt, 0, len(z.Rules))
				for _, r := range z.Rules {
					if r.ID != id {
						filtered = append(filtered, r)
					}
				}
				if _, err := s.Zones.Update(z.ID, z.Name, z.Pattern, z.Purpose, filtered, z.AssignedAgents); err != nil {
					return err
				}
			}
		}
	}
	return s.Prompts.Delete(id)
}

// --- Agent CRUD ---

// ListAgents returns all agents with resolved prompts and skills.
func (s *Service) ListAgents() []*domain.Agent {
	agents := s.Agents.List()
	for _, a := range agents {
		s.ResolveAgentRelations(a)
	}
	return agents
}

// GetAgent returns one agent by id with resolved prompt and skills, or nil if not found.
func (s *Service) GetAgent(id string) *domain.Agent {
	a := s.Agents.Get(id)
	s.ResolveAgentRelations(a)
	return a
}

// CreateAgent creates an agent with the given name, description, prompt ID, skill IDs, and MCP server IDs.
func (s *Service) CreateAgent(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	a, err := s.Agents.Create(name, description, promptID, skillIDs, mcpServerIDs)
	if err != nil {
		return nil, err
	}
	s.ResolveAgentRelations(a)
	return a, nil
}

// UpdateAgent updates an existing agent.
func (s *Service) UpdateAgent(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	a, err := s.Agents.Update(id, name, description, promptID, skillIDs, mcpServerIDs)
	if err != nil {
		return nil, err
	}
	s.ResolveAgentRelations(a)
	return a, nil
}

// DeleteAgent deletes an agent and removes it from all zones that reference it.
func (s *Service) DeleteAgent(id string) error {
	if s.Agents.Get(id) == nil {
		return &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
	}
	for _, p := range s.Projects.List() {
		for _, z := range s.Zones.ListByProject(p.ID) {
			var hasAgent bool
			for _, a := range z.AssignedAgents {
				if a.ID == id {
					hasAgent = true
					break
				}
			}
			if hasAgent {
				filtered := make([]domain.Agent, 0, len(z.AssignedAgents))
				for _, a := range z.AssignedAgents {
					if a.ID != id {
						filtered = append(filtered, a)
					}
				}
				if _, err := s.Zones.Update(z.ID, z.Name, z.Pattern, z.Purpose, z.Rules, filtered); err != nil {
					return err
				}
			}
		}
	}
	return s.Agents.Delete(id)
}

// --- Tool CRUD (user-defined, persisted) ---

func (s *Service) ListTools() []*domain.Tool {
	return s.Tools.List()
}

func (s *Service) GetTool(id string) *domain.Tool {
	return s.Tools.Get(id)
}

func (s *Service) CreateTool(name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	return s.Tools.Create(name, description, inputSchema)
}

func (s *Service) UpdateTool(id, name, description string, inputSchema map[string]any) (*domain.Tool, error) {
	return s.Tools.Update(id, name, description, inputSchema)
}

func (s *Service) DeleteTool(id string) error {
	if s.Tools.Get(id) == nil {
		return &domain.StructuredError{Code: "TOOL_NOT_FOUND", Message: "tool not found"}
	}
	return s.Tools.Delete(id)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// SkillService returns the skill use cases, for callers that build tool groups
// or other surfaces directly on top of them.
func (s *Service) SkillService() *skill.Service { return s.skillSvc }

// MCPServerService returns the MCP server use cases.
func (s *Service) MCPServerService() *mcpserver.Service { return s.mcpSvc }

// ToolStore returns the store backing user-defined tools.
func (s *Service) ToolStore() tool.Store { return s.Tools }

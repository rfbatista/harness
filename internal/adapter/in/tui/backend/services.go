package backend

import (
	"context"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/application/workspaces"
	"operators-mcp/internal/domain"
)

// Services is the production Backend over the application layer.
type Services struct {
	Blueprint     *blueprint.Service
	Planning      *planning.Service
	Orchestration *orchestration.Service
	Workspaces    *workspaces.Service
}

// New wires the real backend.
func New(bp *blueprint.Service, plan *planning.Service, orch *orchestration.Service, ws *workspaces.Service) *Services {
	return &Services{Blueprint: bp, Planning: plan, Orchestration: orch, Workspaces: ws}
}

// --- Reader ---

func (s *Services) ListProjects() []*domain.Project { return s.Blueprint.ListProjects() }

func (s *Services) ListRepositories(projectID string) []*domain.Repository {
	return s.Blueprint.ListRepositories(projectID)
}

func (s *Services) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	return s.Blueprint.ListBoundedContexts(projectID)
}

func (s *Services) ListAgents() []*domain.Agent { return s.Blueprint.ListAgents() }

func (s *Services) ListSkills() []*domain.Skill { return s.Blueprint.ListSkills() }

func (s *Services) GetSkill(id string) *domain.Skill { return s.Blueprint.GetSkill(id) }

func (s *Services) ListMCPServers() []*domain.MCPServer { return s.Blueprint.ListMCPServers() }

// ListTickets fans out over every project when projectID is empty, because the
// planning service only lists per project.
func (s *Services) ListTickets(projectID string) []*domain.Ticket {
	if projectID != "" {
		return s.Planning.ListTickets(projectID)
	}
	var out []*domain.Ticket
	for _, p := range s.Blueprint.ListProjects() {
		out = append(out, s.Planning.ListTickets(p.ID)...)
	}
	return out
}

func (s *Services) ListSessions(f ports.SessionFilter) []*domain.Session {
	return s.Orchestration.List(f)
}

func (s *Services) GetSettings() (map[string]string, error) { return s.Blueprint.GetSettings() }

// --- ProjectWriter ---

func (s *Services) CreateProject(name, rootDir string) (*domain.Project, error) {
	return s.Blueprint.CreateProject(name, rootDir)
}

func (s *Services) UpdateProject(id, name, rootDir string) (*domain.Project, error) {
	return s.Blueprint.UpdateProject(id, name, rootDir)
}

func (s *Services) DeleteProject(id string) error { return s.Blueprint.DeleteProject(id) }

func (s *Services) AddIgnoredPath(projectID, path string) (*domain.Project, error) {
	return s.Blueprint.AddIgnoredPath(projectID, path)
}

func (s *Services) RemoveIgnoredPath(projectID, path string) (*domain.Project, error) {
	return s.Blueprint.RemoveIgnoredPath(projectID, path)
}

func (s *Services) CreateRepository(projectID, name, description, url, rootDir string) (*domain.Repository, error) {
	return s.Blueprint.CreateRepository(projectID, name, description, url, rootDir)
}

func (s *Services) UpdateRepository(id, name, description, url, rootDir string) (*domain.Repository, error) {
	return s.Blueprint.UpdateRepository(id, name, description, url, rootDir)
}

func (s *Services) DeleteRepository(id string) error { return s.Blueprint.DeleteRepository(id) }

func (s *Services) CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	return s.Blueprint.CreateBoundedContext(projectID, name, purpose, terms)
}

func (s *Services) UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	return s.Blueprint.UpdateBoundedContext(id, name, purpose, terms)
}

func (s *Services) DeleteBoundedContext(id string) error { return s.Blueprint.DeleteBoundedContext(id) }

// --- AgentWriter ---

func (s *Services) CreateAgent(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	return s.Blueprint.CreateAgent(name, description, promptID, skillIDs, mcpServerIDs)
}

func (s *Services) UpdateAgent(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	return s.Blueprint.UpdateAgent(id, name, description, promptID, skillIDs, mcpServerIDs)
}

func (s *Services) DeleteAgent(id string) error { return s.Blueprint.DeleteAgent(id) }

// --- SkillWriter ---

func (s *Services) CreateSkill(in domain.SkillInput) (*domain.Skill, error) {
	return s.Blueprint.CreateSkill(in)
}

func (s *Services) UpdateSkill(id string, in domain.SkillInput) (*domain.Skill, error) {
	return s.Blueprint.UpdateSkill(id, in)
}

func (s *Services) DeleteSkill(id string) error { return s.Blueprint.DeleteSkill(id) }

func (s *Services) PutSkillFile(id string, f domain.SkillFile) (*domain.Skill, error) {
	return s.Blueprint.PutSkillFile(id, f)
}

func (s *Services) RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error) {
	return s.Blueprint.RenameSkillFile(id, oldPath, newPath)
}

func (s *Services) DeleteSkillFile(id, path string) (*domain.Skill, error) {
	return s.Blueprint.DeleteSkillFile(id, path)
}

func (s *Services) ValidateSkillPath(path string) (*domain.SkillPathValidation, error) {
	return s.Blueprint.ValidateSkillPath(path)
}

func (s *Services) ImportSkillFromPath(path string) (*domain.Skill, error) {
	return s.Blueprint.ImportSkillFromPath(path)
}

func (s *Services) PublishSkill(id string, force bool) (*domain.Skill, error) {
	return s.Blueprint.PublishSkill(id, force)
}

func (s *Services) UnpublishSkill(id string) (*domain.Skill, error) {
	return s.Blueprint.UnpublishSkill(id)
}

// --- MCPWriter ---

func (s *Services) CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.Blueprint.CreateMCPServer(in)
}

func (s *Services) UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	return s.Blueprint.UpdateMCPServer(id, in)
}

func (s *Services) DeleteMCPServer(id string) error { return s.Blueprint.DeleteMCPServer(id) }

// ProbeMCPServer mirrors the HTTP test_mcp_server handler with persist=true:
// probe the stored configuration, then cache the outcome on the record.
func (s *Services) ProbeMCPServer(ctx context.Context, id string) (domain.MCPProbeResult, error) {
	server := s.Blueprint.GetMCPServer(id)
	if server == nil {
		return domain.MCPProbeResult{}, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "mcp server not found"}
	}
	result, err := s.Blueprint.ProbeMCPServer(ctx, *server)
	if err != nil {
		return result, err
	}
	if _, err := s.Blueprint.UpdateMCPServerProbeResult(id, result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *Services) ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error) {
	return s.Blueprint.ImportMCPServers(content, onDuplicate)
}

// --- SettingsWriter ---

func (s *Services) UpdateSettings(values map[string]string) (map[string]string, error) {
	return s.Blueprint.UpdateSettings(values)
}

// --- TaskWriter ---

func (s *Services) CreateTicket(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	return s.Planning.CreateTicket(projectID, title, description, status)
}

func (s *Services) UpdateTicket(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	return s.Planning.UpdateTicket(id, title, description, status)
}

func (s *Services) DeleteTicket(id string) error { return s.Planning.DeleteTicket(id) }

// --- DocumentReader ---

func (s *Services) ListDocuments(projectID string) []*domain.Document {
	return s.Planning.ListDocuments(projectID)
}

func (s *Services) GetDocument(id string) *domain.Document { return s.Planning.GetDocument(id) }

func (s *Services) ListTicketDocuments(ticketID string) []*domain.Document {
	return s.Planning.ListTicketDocuments(ticketID)
}

// --- Spawner ---

func (s *Services) ListBranches(repositoryID string) ([]domain.GitBranch, error) {
	return s.Workspaces.ListBranches(repositoryID)
}

func (s *Services) StartSession(ctx context.Context, req orchestration.StartRequest) (*domain.Session, error) {
	return s.Orchestration.Start(ctx, req)
}

// --- SessionAccess ---

func (s *Services) GetSession(id string) *domain.Session { return s.Orchestration.Get(id) }

func (s *Services) Subscribe(id string) (<-chan orchestration.SessionEvent, []orchestration.SessionEvent, func()) {
	return s.Orchestration.Subscribe(id)
}

func (s *Services) History(id string, fromSeq int64) []orchestration.SessionEvent {
	return s.Orchestration.History(id, fromSeq)
}

func (s *Services) Send(ctx context.Context, id, text string) error {
	return s.Orchestration.Send(ctx, id, text)
}

func (s *Services) Resolve(ctx context.Context, id, reqID string, allow bool, message string) error {
	return s.Orchestration.Resolve(ctx, id, reqID, allow, message)
}

func (s *Services) Answer(ctx context.Context, id, reqID string, answers, notes map[string]string) error {
	return s.Orchestration.Answer(ctx, id, reqID, answers, notes)
}

func (s *Services) SetAutoRun(ctx context.Context, id string, enabled bool) error {
	return s.Orchestration.SetAutoRun(ctx, id, enabled)
}

func (s *Services) Stop(ctx context.Context, id string) error { return s.Orchestration.Stop(ctx, id) }

func (s *Services) DeleteSession(ctx context.Context, id string) error {
	return s.Orchestration.Delete(ctx, id)
}

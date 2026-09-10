package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Fake is an in-memory Backend for screen tests. Seed the exported slices
// directly; the methods apply the same filters and error codes the real
// services do, so screens can be tested against realistic failures.
type Fake struct {
	Projects        []*domain.Project
	Repositories    []*domain.Repository
	BoundedContexts []*domain.BoundedContext
	Agents          []*domain.Agent
	Skills          []*domain.Skill
	MCPServers      []*domain.MCPServer
	Tickets         []*domain.Ticket
	Sessions        []*domain.Session
	Settings        map[string]string

	// ValidatePath stands in for the filesystem-backed skill path validator.
	ValidatePath func(path string) (*domain.SkillPathValidation, error)
	// Probe stands in for a live MCP connection attempt.
	Probe func(id string) (domain.MCPProbeResult, error)
	// Fail, when set, is returned by every write: the "backend is down" case.
	Fail error

	Documents       []*domain.Document
	TicketDocuments map[string][]string // ticket id → document ids
	Branches        map[string][]domain.GitBranch
	// Started records every StartSession request in order.
	Started []orchestration.StartRequest
	// Sent and Decisions record the session controls the UI exercised.
	Sent      []SentMessage
	Decisions []Decision

	mu   sync.Mutex
	logs map[string]*sessionLog

	seq int
}

// NewFake returns an empty fake with a non-nil settings map.
func NewFake() *Fake { return &Fake{Settings: map[string]string{}} }

func (f *Fake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func notFound(code, what string) error {
	return &domain.StructuredError{Code: code, Message: what + " not found"}
}

func invalid(msg string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg}
}

// --- Reader ---

func (f *Fake) ListProjects() []*domain.Project { return f.Projects }

func (f *Fake) ListRepositories(projectID string) []*domain.Repository {
	var out []*domain.Repository
	for _, r := range f.Repositories {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out
}

func (f *Fake) ListBoundedContexts(projectID string) []*domain.BoundedContext {
	var out []*domain.BoundedContext
	for _, b := range f.BoundedContexts {
		if b.ProjectID == projectID {
			out = append(out, b)
		}
	}
	return out
}

func (f *Fake) ListAgents() []*domain.Agent { return f.Agents }

func (f *Fake) ListSkills() []*domain.Skill { return f.Skills }

func (f *Fake) GetSkill(id string) *domain.Skill {
	for _, s := range f.Skills {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (f *Fake) ListMCPServers() []*domain.MCPServer { return f.MCPServers }

func (f *Fake) ListTickets(projectID string) []*domain.Ticket {
	if projectID == "" {
		return f.Tickets
	}
	var out []*domain.Ticket
	for _, t := range f.Tickets {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out
}

func (f *Fake) ListSessions(filter ports.SessionFilter) []*domain.Session {
	var out []*domain.Session
	for _, s := range f.Sessions {
		if filter.ProjectID != "" && s.ProjectID != filter.ProjectID {
			continue
		}
		if filter.AgentID != "" && s.AgentID != filter.AgentID {
			continue
		}
		if filter.TicketID != "" && s.TicketID != filter.TicketID {
			continue
		}
		if len(filter.Statuses) > 0 && !containsStatus(filter.Statuses, s.Status) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (f *Fake) GetSettings() (map[string]string, error) { return f.Settings, nil }

func containsStatus(list []domain.SessionStatus, st domain.SessionStatus) bool {
	for _, s := range list {
		if s == st {
			return true
		}
	}
	return false
}

// --- ProjectWriter ---

func (f *Fake) project(id string) (int, *domain.Project) {
	for i, p := range f.Projects {
		if p.ID == id {
			return i, p
		}
	}
	return -1, nil
}

func (f *Fake) CreateProject(name, rootDir string) (*domain.Project, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalid("name is required")
	}
	p := &domain.Project{ID: f.nextID("p"), Name: name, RootDir: rootDir}
	f.Projects = append(f.Projects, p)
	return p, nil
}

func (f *Fake) UpdateProject(id, name, rootDir string) (*domain.Project, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	_, p := f.project(id)
	if p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalid("name is required")
	}
	p.Name, p.RootDir = name, rootDir
	return p, nil
}

func (f *Fake) DeleteProject(id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	i, p := f.project(id)
	if p == nil {
		return notFound("PROJECT_NOT_FOUND", "project")
	}
	f.Projects = append(f.Projects[:i], f.Projects[i+1:]...)
	return nil
}

func (f *Fake) AddIgnoredPath(projectID, path string) (*domain.Project, error) {
	_, p := f.project(projectID)
	if p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	p.IgnoredPaths = append(p.IgnoredPaths, path)
	return p, nil
}

func (f *Fake) RemoveIgnoredPath(projectID, path string) (*domain.Project, error) {
	_, p := f.project(projectID)
	if p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	var kept []string
	for _, ip := range p.IgnoredPaths {
		if ip != path {
			kept = append(kept, ip)
		}
	}
	p.IgnoredPaths = kept
	return p, nil
}

func (f *Fake) CreateRepository(projectID, name, description, url, rootDir string) (*domain.Repository, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if _, p := f.project(projectID); p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalid("name is required")
	}
	r := &domain.Repository{ID: f.nextID("r"), ProjectID: projectID, Name: name, Description: description, URL: url, RootDir: rootDir}
	f.Repositories = append(f.Repositories, r)
	return r, nil
}

func (f *Fake) UpdateRepository(id, name, description, url, rootDir string) (*domain.Repository, error) {
	for _, r := range f.Repositories {
		if r.ID == id {
			r.Name, r.Description, r.URL, r.RootDir = name, description, url, rootDir
			return r, nil
		}
	}
	return nil, notFound("REPOSITORY_NOT_FOUND", "repository")
}

func (f *Fake) DeleteRepository(id string) error {
	for i, r := range f.Repositories {
		if r.ID == id {
			f.Repositories = append(f.Repositories[:i], f.Repositories[i+1:]...)
			return nil
		}
	}
	return notFound("REPOSITORY_NOT_FOUND", "repository")
}

func (f *Fake) CreateBoundedContext(projectID, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	if _, p := f.project(projectID); p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalid("name is required")
	}
	bc := &domain.BoundedContext{ID: f.nextID("bc"), ProjectID: projectID, Name: name, Purpose: purpose, UbiquitousLanguage: terms}
	f.BoundedContexts = append(f.BoundedContexts, bc)
	return bc, nil
}

func (f *Fake) UpdateBoundedContext(id, name, purpose string, terms []domain.LanguageTerm) (*domain.BoundedContext, error) {
	for _, bc := range f.BoundedContexts {
		if bc.ID == id {
			bc.Name, bc.Purpose, bc.UbiquitousLanguage = name, purpose, terms
			return bc, nil
		}
	}
	return nil, notFound("BOUNDED_CONTEXT_NOT_FOUND", "bounded context")
}

func (f *Fake) DeleteBoundedContext(id string) error {
	for i, bc := range f.BoundedContexts {
		if bc.ID == id {
			f.BoundedContexts = append(f.BoundedContexts[:i], f.BoundedContexts[i+1:]...)
			return nil
		}
	}
	return notFound("BOUNDED_CONTEXT_NOT_FOUND", "bounded context")
}

// --- AgentWriter ---

func (f *Fake) CreateAgent(name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if strings.TrimSpace(name) == "" {
		return nil, invalid("name is required")
	}
	a := &domain.Agent{ID: f.nextID("a"), Name: name, Description: description, PromptID: promptID, SkillIDs: skillIDs, MCPServerIDs: mcpServerIDs}
	f.Agents = append(f.Agents, a)
	return a, nil
}

func (f *Fake) UpdateAgent(id, name, description, promptID string, skillIDs, mcpServerIDs []string) (*domain.Agent, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	for _, a := range f.Agents {
		if a.ID == id {
			if strings.TrimSpace(name) == "" {
				return nil, invalid("name is required")
			}
			a.Name, a.Description, a.PromptID, a.SkillIDs, a.MCPServerIDs = name, description, promptID, skillIDs, mcpServerIDs
			return a, nil
		}
	}
	return nil, notFound("AGENT_NOT_FOUND", "agent")
}

func (f *Fake) DeleteAgent(id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	for i, a := range f.Agents {
		if a.ID == id {
			f.Agents = append(f.Agents[:i], f.Agents[i+1:]...)
			return nil
		}
	}
	return notFound("AGENT_NOT_FOUND", "agent")
}

// --- SkillWriter ---

func (f *Fake) skill(id string) (int, *domain.Skill) {
	for i, s := range f.Skills {
		if s.ID == id {
			return i, s
		}
	}
	return -1, nil
}

func (f *Fake) CreateSkill(in domain.SkillInput) (*domain.Skill, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, invalid("name is required")
	}
	s := &domain.Skill{ID: f.nextID("k"), Name: in.Name, Description: in.Description, Files: in.Files, License: in.License, Compatibility: in.Compatibility, Metadata: in.Metadata, AllowedTools: in.AllowedTools}
	f.Skills = append(f.Skills, s)
	return s, nil
}

func (f *Fake) UpdateSkill(id string, in domain.SkillInput) (*domain.Skill, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, invalid("name is required")
	}
	s.Name, s.Description, s.License, s.Compatibility, s.Metadata, s.AllowedTools = in.Name, in.Description, in.License, in.Compatibility, in.Metadata, in.AllowedTools
	return s, nil
}

func (f *Fake) DeleteSkill(id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	i, s := f.skill(id)
	if s == nil {
		return notFound("SKILL_NOT_FOUND", "skill")
	}
	f.Skills = append(f.Skills[:i], f.Skills[i+1:]...)
	return nil
}

func (f *Fake) PutSkillFile(id string, file domain.SkillFile) (*domain.Skill, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	for i := range s.Files {
		if s.Files[i].Path == file.Path {
			s.Files[i] = file
			return s, nil
		}
	}
	s.Files = append(s.Files, file)
	return s, nil
}

func (f *Fake) RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error) {
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	for i := range s.Files {
		if s.Files[i].Path == oldPath {
			s.Files[i].Path = newPath
			return s, nil
		}
	}
	return nil, notFound("SKILL_FILE_NOT_FOUND", "skill file")
}

func (f *Fake) DeleteSkillFile(id, path string) (*domain.Skill, error) {
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	for i := range s.Files {
		if s.Files[i].Path == path {
			s.Files = append(s.Files[:i], s.Files[i+1:]...)
			return s, nil
		}
	}
	return nil, notFound("SKILL_FILE_NOT_FOUND", "skill file")
}

func (f *Fake) ValidateSkillPath(path string) (*domain.SkillPathValidation, error) {
	if f.ValidatePath == nil {
		return &domain.SkillPathValidation{Valid: false, Validation: []domain.ValidationIssue{{Level: "error", Message: "no validator configured"}}}, nil
	}
	return f.ValidatePath(path)
}

func (f *Fake) ImportSkillFromPath(path string) (*domain.Skill, error) {
	v, err := f.ValidateSkillPath(path)
	if err != nil {
		return nil, err
	}
	if !v.Valid || v.Preview == nil {
		return nil, invalid("path is not a valid skill")
	}
	s := *v.Preview
	s.ID = f.nextID("k")
	f.Skills = append(f.Skills, &s)
	return &s, nil
}

func (f *Fake) PublishSkill(id string, force bool) (*domain.Skill, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	if s.PublishedPath != "" && !force {
		return nil, &domain.StructuredError{Code: "PUBLISH_TARGET_EXISTS", Message: "publish target already exists"}
	}
	now := time.Now()
	s.PublishedPath = "/published/" + s.Name
	s.PublishedAt = &now
	return s, nil
}

func (f *Fake) UnpublishSkill(id string) (*domain.Skill, error) {
	_, s := f.skill(id)
	if s == nil {
		return nil, notFound("SKILL_NOT_FOUND", "skill")
	}
	s.PublishedPath, s.PublishedAt = "", nil
	return s, nil
}

// --- MCPWriter ---

func (f *Fake) mcp(id string) (int, *domain.MCPServer) {
	for i, s := range f.MCPServers {
		if s.ID == id {
			return i, s
		}
	}
	return -1, nil
}

func (f *Fake) CreateMCPServer(in domain.MCPServerInput) (*domain.MCPServer, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, invalid("name is required")
	}
	s := &domain.MCPServer{ID: f.nextID("m"), Name: in.Name, Description: in.Description, Transport: in.Transport, Command: in.Command, Args: in.Args, URL: in.URL, Env: in.Env, Headers: in.Headers}
	f.MCPServers = append(f.MCPServers, s)
	return s, nil
}

func (f *Fake) UpdateMCPServer(id string, in domain.MCPServerInput) (*domain.MCPServer, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	_, s := f.mcp(id)
	if s == nil {
		return nil, notFound("MCP_SERVER_NOT_FOUND", "mcp server")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, invalid("name is required")
	}
	s.Name, s.Description, s.Transport, s.Command, s.Args, s.URL, s.Env, s.Headers = in.Name, in.Description, in.Transport, in.Command, in.Args, in.URL, in.Env, in.Headers
	return s, nil
}

func (f *Fake) DeleteMCPServer(id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	i, s := f.mcp(id)
	if s == nil {
		return notFound("MCP_SERVER_NOT_FOUND", "mcp server")
	}
	f.MCPServers = append(f.MCPServers[:i], f.MCPServers[i+1:]...)
	return nil
}

func (f *Fake) ProbeMCPServer(_ context.Context, id string) (domain.MCPProbeResult, error) {
	_, s := f.mcp(id)
	if s == nil {
		return domain.MCPProbeResult{}, notFound("MCP_SERVER_NOT_FOUND", "mcp server")
	}
	if f.Probe == nil {
		return domain.MCPProbeResult{Status: domain.MCPProbeStatusUnknown}, nil
	}
	res, err := f.Probe(id)
	if err != nil {
		return res, err
	}
	now := time.Now()
	s.LastProbeAt, s.LastProbeStatus, s.LastProbeError = &now, res.Status, res.Error
	s.ToolCount, s.ResourceCount, s.PromptCount = res.ToolCount, res.ResourceCount, res.PromptCount
	return res, nil
}

// ImportMCPServers understands the Cursor/Claude {"mcpServers": {...}} shape.
func (f *Fake) ImportMCPServers(content, onDuplicate string) ([]*domain.MCPServer, error) {
	switch onDuplicate {
	case "skip", "update", "rename":
	default:
		return nil, invalid("on_duplicate must be skip, update, or rename")
	}
	var doc struct {
		Servers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			URL     string            `json:"url"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil, invalid("content is not valid mcpServers JSON")
	}
	var out []*domain.MCPServer
	for name, cfg := range doc.Servers {
		transport := domain.MCPTransportStdio
		if cfg.URL != "" {
			transport = domain.MCPTransportStreamableHTTP
		}
		existing := f.byName(name)
		switch {
		case existing != nil && onDuplicate == "skip":
			continue
		case existing != nil && onDuplicate == "update":
			existing.Command, existing.Args, existing.URL, existing.Env, existing.Transport = cfg.Command, cfg.Args, cfg.URL, cfg.Env, transport
			out = append(out, existing)
			continue
		case existing != nil:
			name = name + "-2"
		}
		s, err := f.CreateMCPServer(domain.MCPServerInput{Name: name, Transport: transport, Command: cfg.Command, Args: cfg.Args, URL: cfg.URL, Env: cfg.Env})
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *Fake) byName(name string) *domain.MCPServer {
	for _, s := range f.MCPServers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// --- SettingsWriter ---

func (f *Fake) UpdateSettings(values map[string]string) (map[string]string, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	for k, v := range values {
		f.Settings[k] = v
	}
	return f.Settings, nil
}

// --- TaskWriter ---

func (f *Fake) CreateTicket(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if _, p := f.project(projectID); p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	if strings.TrimSpace(title) == "" {
		return nil, invalid("title is required")
	}
	if status == "" {
		status = domain.TicketStatusBacklog
	}
	now := time.Now()
	t := &domain.Ticket{ID: f.nextID("t"), ProjectID: projectID, Title: title, Description: description, Status: status, CreatedAt: now, UpdatedAt: now}
	f.Tickets = append(f.Tickets, t)
	return t, nil
}

func (f *Fake) UpdateTicket(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	for _, t := range f.Tickets {
		if t.ID == id {
			if strings.TrimSpace(title) == "" {
				return nil, invalid("title is required")
			}
			t.Title, t.Description, t.Status, t.UpdatedAt = title, description, status, time.Now()
			return t, nil
		}
	}
	return nil, notFound("TICKET_NOT_FOUND", "ticket")
}

func (f *Fake) DeleteTicket(id string) error {
	if f.Fail != nil {
		return f.Fail
	}
	for i, t := range f.Tickets {
		if t.ID == id {
			f.Tickets = append(f.Tickets[:i], f.Tickets[i+1:]...)
			return nil
		}
	}
	return notFound("TICKET_NOT_FOUND", "ticket")
}

// --- DocumentReader ---

func (f *Fake) ListDocuments(projectID string) []*domain.Document {
	var out []*domain.Document
	for _, d := range f.Documents {
		if projectID == "" || d.ProjectID == projectID {
			out = append(out, d)
		}
	}
	return out
}

func (f *Fake) GetDocument(id string) *domain.Document {
	for _, d := range f.Documents {
		if d.ID == id {
			return d
		}
	}
	return nil
}

func (f *Fake) ListTicketDocuments(ticketID string) []*domain.Document {
	var out []*domain.Document
	for _, id := range f.TicketDocuments[ticketID] {
		if d := f.GetDocument(id); d != nil {
			out = append(out, d)
		}
	}
	return out
}

// --- Spawner ---

func (f *Fake) ListBranches(repositoryID string) ([]domain.GitBranch, error) {
	found := false
	for _, r := range f.Repositories {
		if r.ID == repositoryID {
			found = true
		}
	}
	if !found {
		return nil, notFound("REPOSITORY_NOT_FOUND", "repository")
	}
	return f.Branches[repositoryID], nil
}

// StartSession validates like the orchestration service (project, repository
// and task required; branch unique) and records a starting session.
func (f *Fake) StartSession(_ context.Context, req orchestration.StartRequest) (*domain.Session, error) {
	if f.Fail != nil {
		return nil, f.Fail
	}
	if _, p := f.project(req.ProjectID); p == nil {
		return nil, notFound("PROJECT_NOT_FOUND", "project")
	}
	if req.RepositoryID == "" {
		return nil, invalid("repository_id is required")
	}
	if strings.TrimSpace(req.Task) == "" {
		return nil, invalid("task is required")
	}
	for _, s := range f.Sessions {
		if req.Branch != "" && s.Branch == req.Branch {
			return nil, &domain.StructuredError{Code: "BRANCH_EXISTS", Message: "branch already exists: " + req.Branch}
		}
	}
	f.Started = append(f.Started, req)
	now := time.Now()
	s := &domain.Session{
		ID: f.nextID("s"), ProjectID: req.ProjectID, RepositoryID: req.RepositoryID, AgentID: req.AgentID, TicketID: req.TicketID,
		Task: req.Task, Status: domain.SessionStarting, Branch: req.Branch, CreatedAt: now, UpdatedAt: now,
	}
	f.Sessions = append(f.Sessions, s)
	return s, nil
}

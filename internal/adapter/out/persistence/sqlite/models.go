package sqlite

import (
	"time"

	"operators-mcp/internal/domain"
)

// PromptModel is the GORM model for domain.Prompt.
type PromptModel struct {
	ID          string `gorm:"primaryKey"`
	Name        string
	Description string
	Content     string
}

// TableName overrides the table name.
func (PromptModel) TableName() string { return "prompts" }

// ToDomain converts the model to a domain.Prompt.
func (m *PromptModel) ToDomain() *domain.Prompt {
	if m == nil {
		return nil
	}
	return &domain.Prompt{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		Content:     m.Content,
	}
}

// AgentModel is the GORM model for domain.Agent.
type AgentModel struct {
	ID           string `gorm:"primaryKey"`
	Name         string
	Description  string
	PromptID     string      `gorm:"column:prompt_id"`
	SkillIDs     stringSlice `gorm:"column:skill_ids"`
	MCPServerIDs stringSlice `gorm:"column:mcp_server_ids"`
}

// TableName overrides the table name.
func (AgentModel) TableName() string { return "agents" }

// ToDomain converts the model to a domain.Agent.
func (m *AgentModel) ToDomain() *domain.Agent {
	if m == nil {
		return nil
	}
	skillIDs := []string(m.SkillIDs)
	if skillIDs == nil {
		skillIDs = []string{}
	}
	mcpServerIDs := []string(m.MCPServerIDs)
	if mcpServerIDs == nil {
		mcpServerIDs = []string{}
	}
	return &domain.Agent{
		ID:           m.ID,
		Name:         m.Name,
		Description:  m.Description,
		PromptID:     m.PromptID,
		SkillIDs:     skillIDs,
		MCPServerIDs: mcpServerIDs,
	}
}

// SkillModel is the GORM model for domain.Skill.
type SkillModel struct {
	ID            string `gorm:"primaryKey"`
	Name          string
	Description   string
	Content       string
	Path          string
	License       string
	Compatibility string
	Metadata      stringMap `gorm:"column:metadata"`
	AllowedTools  string    `gorm:"column:allowed_tools"`
	PublishedSlug string    `gorm:"column:published_slug"`
	PublishedAt   time.Time `gorm:"column:published_at"`
	PublishError  string    `gorm:"column:publish_error"`
}

// TableName overrides the table name.
func (SkillModel) TableName() string { return "skills" }

// ToDomain converts the model to a domain.Skill.
func (m *SkillModel) ToDomain() *domain.Skill {
	if m == nil {
		return nil
	}
	meta := map[string]string(m.Metadata)
	if len(meta) == 0 {
		meta = nil
	}
	var publishedAt *time.Time
	if !m.PublishedAt.IsZero() {
		at := m.PublishedAt
		publishedAt = &at
	}
	return &domain.Skill{
		ID:            m.ID,
		Name:          m.Name,
		Description:   m.Description,
		Content:       m.Content,
		Path:          m.Path,
		License:       m.License,
		Compatibility: m.Compatibility,
		Metadata:      meta,
		AllowedTools:  m.AllowedTools,
		PublishedSlug: m.PublishedSlug,
		PublishedAt:   publishedAt,
		PublishError:  m.PublishError,
	}
}

func skillModelFromInput(id string, in domain.SkillInput) *SkillModel {
	meta := stringMap(in.Metadata)
	if meta == nil {
		meta = stringMap{}
	}
	return &SkillModel{
		ID:            id,
		Name:          in.Name,
		Description:   in.Description,
		Content:       in.Content,
		Path:          in.Path,
		License:       in.License,
		Compatibility: in.Compatibility,
		Metadata:      meta,
		AllowedTools:  in.AllowedTools,
	}
}

// SkillFileModel is one node of a skill's stored directory tree.
type SkillFileModel struct {
	ID       string `gorm:"primaryKey"`
	SkillID  string `gorm:"index:idx_skill_files_skill_path,unique,priority:1;index"`
	Path     string `gorm:"index:idx_skill_files_skill_path,unique,priority:2"`
	Dir      bool
	Content  string
	Encoding string
}

// TableName overrides the table name.
func (SkillFileModel) TableName() string { return "skill_files" }

// ToDomain converts the model to a domain.SkillFile.
func (m *SkillFileModel) ToDomain() domain.SkillFile {
	return domain.SkillFile{
		Path:     m.Path,
		Dir:      m.Dir,
		Content:  m.Content,
		Encoding: m.Encoding,
	}
}

func skillFileModel(id, skillID string, f domain.SkillFile) *SkillFileModel {
	return &SkillFileModel{
		ID:       id,
		SkillID:  skillID,
		Path:     f.Path,
		Dir:      f.Dir,
		Content:  f.Content,
		Encoding: f.Encoding,
	}
}

// SettingModel is one global key/value app setting.
type SettingModel struct {
	Key   string `gorm:"primaryKey"`
	Value string
}

// TableName overrides the table name.
func (SettingModel) TableName() string { return "settings" }

// ProjectModel is the GORM model for domain.Project.
type ProjectModel struct {
	ID           string `gorm:"primaryKey"`
	Name         string
	RootDir      string      `gorm:"column:root_dir"`
	IgnoredPaths stringSlice `gorm:"column:ignored_paths"`
}

// TableName overrides the table name.
func (ProjectModel) TableName() string { return "projects" }

// ToDomain converts the model to a domain.Project.
func (m *ProjectModel) ToDomain() *domain.Project {
	if m == nil {
		return nil
	}
	paths := []string(m.IgnoredPaths)
	if paths == nil {
		paths = []string{}
	}
	return &domain.Project{
		ID:           m.ID,
		Name:         m.Name,
		RootDir:      m.RootDir,
		IgnoredPaths: paths,
	}
}

// RepositoryModel is the GORM model for domain.Repository.
// Repositories are scoped to a project via ProjectID (one-to-many).
type RepositoryModel struct {
	ID           string `gorm:"primaryKey"`
	ProjectID    string `gorm:"column:project_id;index"`
	Name         string
	Description  string
	URL          string      `gorm:"column:url"`
	RootDir      string      `gorm:"column:root_dir"`
	IgnoredPaths stringSlice `gorm:"column:ignored_paths"`
}

// TableName overrides the table name.
func (RepositoryModel) TableName() string { return "repositories" }

// EnvFileModel is one env file of a repository (domain.EnvFile), keyed by the
// repository and the file's path in it.
type EnvFileModel struct {
	RepositoryID string `gorm:"column:repository_id;primaryKey"`
	Path         string `gorm:"primaryKey"`
	Content      string
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (EnvFileModel) TableName() string { return "repository_env_files" }

// RunCommandModel is one saved run command of a repository (domain.RunCommand).
type RunCommandModel struct {
	RepositoryID string `gorm:"column:repository_id;primaryKey"`
	Name         string `gorm:"primaryKey"`
	Command      string
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (RunCommandModel) TableName() string { return "repository_run_commands" }

// ToDomain converts the model to a domain.Repository.
func (m *RepositoryModel) ToDomain() *domain.Repository {
	if m == nil {
		return nil
	}
	paths := []string(m.IgnoredPaths)
	if paths == nil {
		paths = []string{}
	}
	return &domain.Repository{
		ID:           m.ID,
		ProjectID:    m.ProjectID,
		Name:         m.Name,
		Description:  m.Description,
		URL:          m.URL,
		RootDir:      m.RootDir,
		IgnoredPaths: paths,
	}
}

// ZoneModel is the GORM model for domain.Zone.
type ZoneModel struct {
	ID               string `gorm:"primaryKey"`
	ProjectID        string `gorm:"column:project_id;index"`
	BoundedContextID string `gorm:"column:bounded_context_id;index"`
	Name             string
	Pattern          string
	Purpose          string
	Rules            promptSlice `gorm:"column:rules"`
	AssignedAgents   agentSlice  `gorm:"column:assigned_agents"`
	ExplicitPaths    stringSlice `gorm:"column:explicit_paths"`
}

// TableName overrides the table name.
func (ZoneModel) TableName() string { return "zones" }

// ToDomain converts the model to a domain.Zone.
func (m *ZoneModel) ToDomain() *domain.Zone {
	if m == nil {
		return nil
	}
	return &domain.Zone{
		ID:               m.ID,
		ProjectID:        m.ProjectID,
		BoundedContextID: m.BoundedContextID,
		Name:             m.Name,
		Pattern:          m.Pattern,
		Purpose:          m.Purpose,
		Rules:            slicePromptsOrNil([]domain.Prompt(m.Rules)),
		AssignedAgents:   sliceAgentsOrNil([]domain.Agent(m.AssignedAgents)),
		ExplicitPaths:    sliceOrNil([]string(m.ExplicitPaths)),
	}
}

// BoundedContextModel is the GORM model for domain.BoundedContext.
type BoundedContextModel struct {
	ID                 string `gorm:"primaryKey"`
	ProjectID          string `gorm:"column:project_id;index"`
	Name               string
	Purpose            string
	UbiquitousLanguage languageTermSlice `gorm:"column:ubiquitous_language"`
}

// TableName overrides the table name.
func (BoundedContextModel) TableName() string { return "bounded_contexts" }

// ToDomain converts the model to a domain.BoundedContext.
func (m *BoundedContextModel) ToDomain() *domain.BoundedContext {
	if m == nil {
		return nil
	}
	return &domain.BoundedContext{
		ID:                 m.ID,
		ProjectID:          m.ProjectID,
		Name:               m.Name,
		Purpose:            m.Purpose,
		UbiquitousLanguage: sliceTermsOrNil([]domain.LanguageTerm(m.UbiquitousLanguage)),
	}
}

// MCPServerModel is the GORM model for domain.MCPServer.
type MCPServerModel struct {
	ID          string `gorm:"primaryKey"`
	Name        string
	Description string
	Transport   string
	Command     string
	Args        stringSlice `gorm:"column:args"`
	URL         string      `gorm:"column:url"`
	Env         stringMap   `gorm:"column:env"`
	Headers     stringMap   `gorm:"column:headers"`

	LastProbeAt     *time.Time `gorm:"column:last_probe_at"`
	LastProbeStatus string     `gorm:"column:last_probe_status"`
	LastProbeError  string     `gorm:"column:last_probe_error"`
	ToolCount       int        `gorm:"column:tool_count"`
	ResourceCount   int        `gorm:"column:resource_count"`
	PromptCount     int        `gorm:"column:prompt_count"`
}

// TableName overrides the table name.
func (MCPServerModel) TableName() string { return "mcp_servers" }

// ToDomain converts the model to a domain.MCPServer.
func (m *MCPServerModel) ToDomain() *domain.MCPServer {
	if m == nil {
		return nil
	}
	args := []string(m.Args)
	if args == nil {
		args = []string{}
	}
	env := map[string]string(m.Env)
	if env == nil {
		env = map[string]string{}
	}
	headers := map[string]string(m.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	return &domain.MCPServer{
		ID:              m.ID,
		Name:            m.Name,
		Description:     m.Description,
		Transport:       m.Transport,
		Command:         m.Command,
		Args:            args,
		URL:             m.URL,
		Env:             env,
		Headers:         headers,
		LastProbeAt:     m.LastProbeAt,
		LastProbeStatus: m.LastProbeStatus,
		LastProbeError:  m.LastProbeError,
		ToolCount:       m.ToolCount,
		ResourceCount:   m.ResourceCount,
		PromptCount:     m.PromptCount,
	}
}

// ToolModel is the GORM model for domain.Tool (user-defined tools only).
type ToolModel struct {
	ID          string `gorm:"primaryKey"`
	Name        string
	Description string
	InputSchema jsonMap `gorm:"column:input_schema"`
}

func (ToolModel) TableName() string { return "tools" }

func (m *ToolModel) ToDomain() *domain.Tool {
	if m == nil {
		return nil
	}
	schema := map[string]any(m.InputSchema)
	if schema == nil {
		schema = map[string]any{}
	}
	return &domain.Tool{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		InputSchema: schema,
		Source:      "user",
	}
}

// TaskModel is the GORM model for domain.Task.
type TaskModel struct {
	ID          string `gorm:"primaryKey"`
	ZoneID      string `gorm:"column:zone_id;index"`
	AgentID     string `gorm:"column:agent_id;index"`
	ProjectID   string `gorm:"column:project_id;index"`
	Instruction string
	Status      string
	Result      string
	Error       string
	CreatedAt   int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt   int64 `gorm:"autoUpdateTime:milli"`
}

func (TaskModel) TableName() string { return "tasks" }

func (m *TaskModel) ToDomain() *domain.Task {
	if m == nil {
		return nil
	}
	return &domain.Task{
		ID:          m.ID,
		ZoneID:      m.ZoneID,
		AgentID:     m.AgentID,
		ProjectID:   m.ProjectID,
		Instruction: m.Instruction,
		Status:      domain.TaskStatus(m.Status),
		Result:      m.Result,
		Error:       m.Error,
		CreatedAt:   time.UnixMilli(m.CreatedAt),
		UpdatedAt:   time.UnixMilli(m.UpdatedAt),
	}
}

// TicketModel is the GORM model for domain.Ticket.
type TicketModel struct {
	ID          string `gorm:"primaryKey"`
	ProjectID   string `gorm:"column:project_id;index"`
	Title       string
	Description string
	Status      string
	CreatedAt   int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt   int64 `gorm:"autoUpdateTime:milli"`
}

func (TicketModel) TableName() string { return "tickets" }

func (m *TicketModel) ToDomain() *domain.Ticket {
	if m == nil {
		return nil
	}
	return &domain.Ticket{
		ID:          m.ID,
		ProjectID:   m.ProjectID,
		Title:       m.Title,
		Description: m.Description,
		Status:      domain.TicketStatus(m.Status),
		CreatedAt:   time.UnixMilli(m.CreatedAt),
		UpdatedAt:   time.UnixMilli(m.UpdatedAt),
	}
}

// DocumentModel is the GORM model for domain.Document.
type DocumentModel struct {
	ID        string `gorm:"primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Title     string
	Content   string
	CreatedAt int64 `gorm:"autoCreateTime:milli"`
	UpdatedAt int64 `gorm:"autoUpdateTime:milli"`
}

func (DocumentModel) TableName() string { return "documents" }

func (m *DocumentModel) ToDomain() *domain.Document {
	if m == nil {
		return nil
	}
	return &domain.Document{
		ID:        m.ID,
		ProjectID: m.ProjectID,
		Title:     m.Title,
		Content:   m.Content,
		CreatedAt: time.UnixMilli(m.CreatedAt),
		UpdatedAt: time.UnixMilli(m.UpdatedAt),
	}
}

// TicketDocumentModel is the join table linking tickets and documents (many-to-many).
type TicketDocumentModel struct {
	TicketID   string `gorm:"column:ticket_id;primaryKey;index"`
	DocumentID string `gorm:"column:document_id;primaryKey;index"`
}

func (TicketDocumentModel) TableName() string { return "ticket_documents" }

func sliceOrNil(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

func sliceAgentsOrNil(a []domain.Agent) []domain.Agent {
	if a == nil {
		return nil
	}
	return append([]domain.Agent(nil), a...)
}

func slicePromptsOrNil(p []domain.Prompt) []domain.Prompt {
	if p == nil {
		return nil
	}
	return append([]domain.Prompt(nil), p...)
}

func sliceTermsOrNil(t []domain.LanguageTerm) []domain.LanguageTerm {
	if t == nil {
		return nil
	}
	return append([]domain.LanguageTerm(nil), t...)
}

// WorkspaceModel is the GORM model for domain.Workspace. The composite unique
// index enforces name uniqueness within a repository at the database level.
type WorkspaceModel struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"column:repository_id;index;uniqueIndex:idx_workspaces_repo_name"`
	Name         string `gorm:"uniqueIndex:idx_workspaces_repo_name"`
	Branch       string
	Path         string
	BaseRef      string `gorm:"column:base_ref"`
	CreatedAt    time.Time
}

// TableName overrides the table name.
func (WorkspaceModel) TableName() string { return "workspaces" }

// ToDomain converts the model to a domain.Workspace.
func (m *WorkspaceModel) ToDomain() *domain.Workspace {
	if m == nil {
		return nil
	}
	return &domain.Workspace{
		ID:           m.ID,
		RepositoryID: m.RepositoryID,
		Name:         m.Name,
		Branch:       m.Branch,
		Path:         m.Path,
		BaseRef:      m.BaseRef,
		CreatedAt:    m.CreatedAt,
	}
}

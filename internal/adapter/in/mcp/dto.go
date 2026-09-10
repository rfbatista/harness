package mcp

import (
	"time"

	"operators-mcp/internal/domain"
)

// PromptDTO is the MCP/JSON representation of a prompt.
type PromptDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
}

// PromptToDTO converts a domain Prompt to API DTO.
func PromptToDTO(p *domain.Prompt) *PromptDTO {
	if p == nil {
		return nil
	}
	return &PromptDTO{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Content:     p.Content,
	}
}

// PromptsToDTO converts domain prompts to DTOs.
func PromptsToDTO(ps []*domain.Prompt) []*PromptDTO {
	out := make([]*PromptDTO, len(ps))
	for i, p := range ps {
		out[i] = PromptToDTO(p)
	}
	return out
}

// SkillDTO is the MCP/JSON representation of an AI-agent skill.
type SkillDTO struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Description   string                   `json:"description,omitempty"`
	Files         []SkillFileDTO           `json:"files,omitempty"`
	License       string                   `json:"license,omitempty"`
	Compatibility string                   `json:"compatibility,omitempty"`
	Metadata      map[string]string        `json:"metadata,omitempty"`
	AllowedTools  string                   `json:"allowed_tools,omitempty"`
	Resources     []string                 `json:"resources,omitempty"`
	Validation    []domain.ValidationIssue `json:"validation,omitempty"`

	PublishedPath string     `json:"published_path,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	PublishError  string     `json:"publish_error,omitempty"`

	// Deprecated: retained for back-compat with older clients.
	Content string `json:"content,omitempty"`
	Path    string `json:"path,omitempty"`
}

// SkillFileDTO is one node of a skill's directory tree.
type SkillFileDTO struct {
	Path     string `json:"path"`
	Dir      bool   `json:"dir,omitempty"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// SkillFileToDTO converts a domain SkillFile to its DTO.
func SkillFileToDTO(f domain.SkillFile) SkillFileDTO {
	return SkillFileDTO{Path: f.Path, Dir: f.Dir, Content: f.Content, Encoding: f.Encoding}
}

// SkillFilesToDTO converts a slice of domain SkillFiles.
func SkillFilesToDTO(files []domain.SkillFile) []SkillFileDTO {
	if len(files) == 0 {
		return nil
	}
	out := make([]SkillFileDTO, len(files))
	for i, f := range files {
		out[i] = SkillFileToDTO(f)
	}
	return out
}

// SkillFileFromDTO converts a DTO node to a domain SkillFile.
func SkillFileFromDTO(f SkillFileDTO) domain.SkillFile {
	return domain.SkillFile{Path: f.Path, Dir: f.Dir, Content: f.Content, Encoding: f.Encoding}
}

// SkillFilesFromDTO converts a slice of DTO nodes.
func SkillFilesFromDTO(files []SkillFileDTO) []domain.SkillFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]domain.SkillFile, len(files))
	for i, f := range files {
		out[i] = SkillFileFromDTO(f)
	}
	return out
}

// SkillToDTO converts a domain Skill to API DTO.
func SkillToDTO(s *domain.Skill) *SkillDTO {
	if s == nil {
		return nil
	}
	meta := s.Metadata
	if len(meta) == 0 {
		meta = nil
	}
	return &SkillDTO{
		ID:            s.ID,
		Name:          s.Name,
		Description:   s.Description,
		Files:         SkillFilesToDTO(s.Files),
		License:       s.License,
		Compatibility: s.Compatibility,
		Metadata:      meta,
		AllowedTools:  s.AllowedTools,
		Resources:     append([]string(nil), s.Resources...),
		Validation:    append([]domain.ValidationIssue(nil), s.Validation...),
		PublishedPath: s.PublishedPath,
		PublishedAt:   s.PublishedAt,
		PublishError:  s.PublishError,
	}
}

// SkillsToDTO converts domain skills to DTOs.
func SkillsToDTO(skills []*domain.Skill) []*SkillDTO {
	out := make([]*SkillDTO, len(skills))
	for i, skill := range skills {
		out[i] = SkillToDTO(skill)
	}
	return out
}

// AgentDTO is the MCP/JSON representation of an agent.
type AgentDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	PromptID     string          `json:"prompt_id,omitempty"`
	SkillIDs     []string        `json:"skill_ids,omitempty"`
	MCPServerIDs []string        `json:"mcp_server_ids,omitempty"`
	Prompt       *PromptDTO      `json:"prompt,omitempty"`
	Skills       []*SkillDTO     `json:"skills,omitempty"`
	MCPServers   []*MCPServerDTO `json:"mcp_servers,omitempty"`
}

// AgentToDTO converts a domain Agent to API DTO.
func AgentToDTO(a *domain.Agent) *AgentDTO {
	if a == nil {
		return nil
	}
	return &AgentDTO{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		PromptID:     a.PromptID,
		SkillIDs:     append([]string(nil), a.SkillIDs...),
		MCPServerIDs: append([]string(nil), a.MCPServerIDs...),
		Prompt:       PromptToDTO(a.Prompt),
		Skills:       SkillRefsToDTO(a.Skills),
		MCPServers:   MCPServerRefsToDTO(a.MCPServers),
	}
}

// MCPServerDTO is the MCP/JSON representation of an MCP server configuration.
type MCPServerDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`

	LastProbeAt     *time.Time `json:"last_probe_at,omitempty"`
	LastProbeStatus string     `json:"last_probe_status,omitempty"`
	LastProbeError  string     `json:"last_probe_error,omitempty"`
	ToolCount       int        `json:"tool_count,omitempty"`
	ResourceCount   int        `json:"resource_count,omitempty"`
	PromptCount     int        `json:"prompt_count,omitempty"`
	AgentCount      int        `json:"agent_count,omitempty"`
	AgentNames      []string   `json:"agent_names,omitempty"`
}

// MCPServerToDTO converts a domain MCPServer to API DTO.
func MCPServerToDTO(m *domain.MCPServer) *MCPServerDTO {
	if m == nil {
		return nil
	}
	return &MCPServerDTO{
		ID:              m.ID,
		Name:            m.Name,
		Description:     m.Description,
		Transport:       m.Transport,
		Command:         m.Command,
		Args:            m.Args,
		URL:             m.URL,
		Env:             m.Env,
		Headers:         m.Headers,
		LastProbeAt:     m.LastProbeAt,
		LastProbeStatus: m.LastProbeStatus,
		LastProbeError:  m.LastProbeError,
		ToolCount:       m.ToolCount,
		ResourceCount:   m.ResourceCount,
		PromptCount:     m.PromptCount,
		AgentCount:      m.AgentCount,
		AgentNames:      append([]string(nil), m.AgentNames...),
	}
}

// MCPServersToDTO converts domain MCP servers to DTOs.
func MCPServersToDTO(servers []*domain.MCPServer) []*MCPServerDTO {
	out := make([]*MCPServerDTO, len(servers))
	for i, s := range servers {
		out[i] = MCPServerToDTO(s)
	}
	return out
}

// ProbeResultToDTO converts a domain probe result to API DTO.
func ProbeResultToDTO(r domain.MCPProbeResult) *MCPProbeResultDTO {
	return &MCPProbeResultDTO{
		Status:        r.Status,
		Error:         r.Error,
		ToolCount:     r.ToolCount,
		ResourceCount: r.ResourceCount,
		PromptCount:   r.PromptCount,
		ToolNames:     append([]string(nil), r.ToolNames...),
		ServerName:    r.ServerName,
		ServerVersion: r.ServerVersion,
	}
}

// ProjectDTO is the MCP/JSON representation of a project (snake_case for API contract).
type ProjectDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	RootDir      string   `json:"root_dir"`
	IgnoredPaths []string `json:"ignored_paths,omitempty"`
}

// ZoneDTO is the MCP/JSON representation of a zone (snake_case for API contract).
type ZoneDTO struct {
	ID             string      `json:"id"`
	ProjectID      string      `json:"project_id"`
	Name           string      `json:"name"`
	Pattern        string      `json:"pattern"`
	Purpose        string      `json:"purpose"`
	Rules          []PromptDTO `json:"rules"`
	AssignedAgents []AgentDTO  `json:"assigned_agents"`
	ExplicitPaths  []string    `json:"explicit_paths"`
}

// TreeNodeDTO is the MCP/JSON representation of a tree node.
type TreeNodeDTO struct {
	Path     string         `json:"path"`
	Name     string         `json:"name"`
	IsDir    bool           `json:"is_dir"`
	Children []*TreeNodeDTO `json:"children"`
}

// ProjectToDTO converts a domain Project to API DTO (exported for HTTP adapter).
func ProjectToDTO(p *domain.Project) *ProjectDTO {
	if p == nil {
		return nil
	}
	ignored := p.IgnoredPaths
	if len(ignored) == 0 {
		ignored = nil
	}
	return &ProjectDTO{
		ID:           p.ID,
		Name:         p.Name,
		RootDir:      p.RootDir,
		IgnoredPaths: ignored,
	}
}

// ProjectsToDTO converts domain projects to DTOs.
func ProjectsToDTO(projects []*domain.Project) []*ProjectDTO {
	out := make([]*ProjectDTO, len(projects))
	for i, p := range projects {
		out[i] = ProjectToDTO(p)
	}
	return out
}

// ZoneToDTO converts a domain Zone to API DTO (exported for HTTP adapter).
func ZoneToDTO(z *domain.Zone) *ZoneDTO {
	if z == nil {
		return nil
	}
	return &ZoneDTO{
		ID:             z.ID,
		ProjectID:      z.ProjectID,
		Name:           z.Name,
		Pattern:        z.Pattern,
		Purpose:        z.Purpose,
		Rules:          RulesToDTO(z.Rules),
		AssignedAgents: AgentsToDTO(z.AssignedAgents),
		ExplicitPaths:  append([]string(nil), z.ExplicitPaths...),
	}
}

// RulesToDTO converts domain prompts (zone rules) to DTOs.
func RulesToDTO(rules []domain.Prompt) []PromptDTO {
	if len(rules) == 0 {
		return nil
	}
	out := make([]PromptDTO, len(rules))
	for i := range rules {
		out[i] = PromptDTO{ID: rules[i].ID, Name: rules[i].Name}
	}
	return out
}

// DTOToRules converts PromptDTO slice to domain prompts (only ID+Name for storage).
func DTOToRules(dtos []PromptDTO) []domain.Prompt {
	if len(dtos) == 0 {
		return nil
	}
	out := make([]domain.Prompt, len(dtos))
	for i := range dtos {
		out[i] = domain.Prompt{ID: dtos[i].ID, Name: dtos[i].Name}
	}
	return out
}

// SkillRefsToDTO converts resolved domain skills to DTOs.
func SkillRefsToDTO(skills []domain.Skill) []*SkillDTO {
	if len(skills) == 0 {
		return nil
	}
	out := make([]*SkillDTO, len(skills))
	for i := range skills {
		skill := skills[i]
		out[i] = SkillToDTO(&skill)
	}
	return out
}

// MCPServerRefsToDTO converts resolved domain MCP servers to DTOs.
func MCPServerRefsToDTO(servers []domain.MCPServer) []*MCPServerDTO {
	if len(servers) == 0 {
		return nil
	}
	out := make([]*MCPServerDTO, len(servers))
	for i := range servers {
		server := servers[i]
		out[i] = MCPServerToDTO(&server)
	}
	return out
}

// AgentsToDTO converts domain agents to DTOs (exported for HTTP adapter).
func AgentsToDTO(a []domain.Agent) []AgentDTO {
	if len(a) == 0 {
		return nil
	}
	out := make([]AgentDTO, len(a))
	for i := range a {
		out[i] = AgentDTO{ID: a[i].ID, Name: a[i].Name}
	}
	return out
}

// DTOToAgents converts AgentDTO slice to domain agents (exported for HTTP adapter).
func DTOToAgents(a []AgentDTO) []domain.Agent {
	if len(a) == 0 {
		return nil
	}
	out := make([]domain.Agent, len(a))
	for i := range a {
		out[i] = domain.Agent{ID: a[i].ID, Name: a[i].Name}
	}
	return out
}

// ZonesToDTO converts domain zones to DTOs (exported for HTTP adapter).
func ZonesToDTO(zones []*domain.Zone) []*ZoneDTO {
	out := make([]*ZoneDTO, len(zones))
	for i, z := range zones {
		out[i] = ZoneToDTO(z)
	}
	return out
}

// TreeNodeToDTO converts a domain TreeNode to API DTO (exported for HTTP adapter).
func TreeNodeToDTO(n *domain.TreeNode) *TreeNodeDTO {
	if n == nil {
		return nil
	}
	children := make([]*TreeNodeDTO, len(n.Children))
	for i, c := range n.Children {
		children[i] = TreeNodeToDTO(c)
	}
	return &TreeNodeDTO{
		Path:     n.Path,
		Name:     n.Name,
		IsDir:    n.IsDir,
		Children: children,
	}
}

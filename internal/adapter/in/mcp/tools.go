package mcp

import "operators-mcp/internal/domain"

// ListMatchingPathsIn is the input for list_matching_paths.
type ListMatchingPathsIn struct {
	Pattern   string `json:"pattern" jsonschema:"required"`
	Root      string `json:"root,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// ListMatchingPathsOut is the output for list_matching_paths.
type ListMatchingPathsOut struct {
	Paths []string `json:"paths"`
}

// ListTreeIn is the input for list_tree.
type ListTreeIn struct {
	Root      string `json:"root,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Depth     int    `json:"depth,omitempty"`
}

// ListTreeOut is the output for list_tree.
type ListTreeOut struct {
	Tree any `json:"tree"`
}

// ListZonesIn is the input for list_zones.
type ListZonesIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
}

// ListZonesOut is the output for list_zones.
type ListZonesOut struct {
	Zones []*ZoneDTO `json:"zones"`
}

// ListProjectsOut is the output for list_projects.
type ListProjectsOut struct {
	Projects []*ProjectDTO `json:"projects"`
}

// GetProjectIn is the input for get_project.
type GetProjectIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
}

// GetProjectOut is the output for get_project.
type GetProjectOut struct {
	Project *ProjectDTO `json:"project"`
}

// CreateProjectIn is the input for create_project.
type CreateProjectIn struct {
	Name    string `json:"name,omitempty"`
	RootDir string `json:"root_dir" jsonschema:"required"`
}

// CreateProjectOut is the output for create_project.
type CreateProjectOut struct {
	Project *ProjectDTO `json:"project"`
}

// UpdateProjectIn is the input for update_project.
type UpdateProjectIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
	Name      string `json:"name,omitempty"`
	RootDir   string `json:"root_dir,omitempty"`
}

// UpdateProjectOut is the output for update_project.
type UpdateProjectOut struct {
	Project *ProjectDTO `json:"project"`
}

// DeleteProjectIn is the input for delete_project.
type DeleteProjectIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
}

// AddIgnoredPathIn is the input for add_ignored_path.
type AddIgnoredPathIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
	Path      string `json:"path" jsonschema:"required"`
}

// AddIgnoredPathOut is the output for add_ignored_path.
type AddIgnoredPathOut struct {
	Project *ProjectDTO `json:"project"`
}

// RemoveIgnoredPathIn is the input for remove_ignored_path.
type RemoveIgnoredPathIn struct {
	ProjectID string `json:"project_id" jsonschema:"required"`
	Path      string `json:"path" jsonschema:"required"`
}

// RemoveIgnoredPathOut is the output for remove_ignored_path.
type RemoveIgnoredPathOut struct {
	Project *ProjectDTO `json:"project"`
}

// GetZoneIn is the input for get_zone.
type GetZoneIn struct {
	ZoneID string `json:"zone_id" jsonschema:"required"`
}

// GetZoneOut is the output for get_zone.
type GetZoneOut struct {
	Zone *ZoneDTO `json:"zone"`
}

// CreateZoneIn is the input for create_zone.
type CreateZoneIn struct {
	ProjectID      string      `json:"project_id" jsonschema:"required"`
	Name           string      `json:"name" jsonschema:"required"`
	Pattern        string      `json:"pattern,omitempty"`
	Purpose        string      `json:"purpose,omitempty"`
	Rules          []PromptDTO `json:"rules,omitempty"`
	AssignedAgents []AgentDTO  `json:"assigned_agents,omitempty"`
}

// CreateZoneOut is the output for create_zone.
type CreateZoneOut struct {
	Zone *ZoneDTO `json:"zone"`
}

// UpdateZoneIn is the input for update_zone.
type UpdateZoneIn struct {
	ZoneID         string      `json:"zone_id" jsonschema:"required"`
	Name           string      `json:"name,omitempty"`
	Pattern        string      `json:"pattern,omitempty"`
	Purpose        string      `json:"purpose,omitempty"`
	Rules          []PromptDTO `json:"rules,omitempty"`
	AssignedAgents []AgentDTO  `json:"assigned_agents,omitempty"`
}

// UpdateZoneOut is the output for update_zone.
type UpdateZoneOut struct {
	Zone *ZoneDTO `json:"zone"`
}

// AssignPathToZoneIn is the input for assign_path_to_zone.
type AssignPathToZoneIn struct {
	ZoneID string `json:"zone_id" jsonschema:"required"`
	Path   string `json:"path" jsonschema:"required"`
}

// AssignPathToZoneOut is the output for assign_path_to_zone.
type AssignPathToZoneOut struct {
	Zone *ZoneDTO `json:"zone"`
}

// RemovePathFromZoneIn is the input for remove_path_from_zone.
type RemovePathFromZoneIn struct {
	ZoneID string `json:"zone_id" jsonschema:"required"`
	Path   string `json:"path" jsonschema:"required"`
}

// RemovePathFromZoneOut is the output for remove_path_from_zone.
type RemovePathFromZoneOut struct {
	Zone *ZoneDTO `json:"zone"`
}

// --- Agent tool structs ---

// ListAgentsOut is the output for list_agents.
type ListAgentsOut struct {
	Agents []*AgentDTO `json:"agents"`
}

// GetAgentIn is the input for get_agent.
type GetAgentIn struct {
	AgentID string `json:"agent_id" jsonschema:"required"`
}

// GetAgentOut is the output for get_agent.
type GetAgentOut struct {
	Agent *AgentDTO `json:"agent"`
}

// CreateAgentIn is the input for create_agent.
type CreateAgentIn struct {
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	PromptID     string   `json:"prompt_id,omitempty"`
	SkillIDs     []string `json:"skill_ids,omitempty"`
	MCPServerIDs []string `json:"mcp_server_ids,omitempty"`
}

// CreateAgentOut is the output for create_agent.
type CreateAgentOut struct {
	Agent *AgentDTO `json:"agent"`
}

// UpdateAgentIn is the input for update_agent.
type UpdateAgentIn struct {
	AgentID      string   `json:"agent_id" jsonschema:"required"`
	Name         string   `json:"name,omitempty"`
	Description  string   `json:"description,omitempty"`
	PromptID     string   `json:"prompt_id,omitempty"`
	SkillIDs     []string `json:"skill_ids,omitempty"`
	MCPServerIDs []string `json:"mcp_server_ids,omitempty"`
}

// UpdateAgentOut is the output for update_agent.
type UpdateAgentOut struct {
	Agent *AgentDTO `json:"agent"`
}

// DeleteAgentIn is the input for delete_agent.
type DeleteAgentIn struct {
	AgentID string `json:"agent_id" jsonschema:"required"`
}

// --- Prompt tool structs ---

// ListPromptsOut is the output for list_prompts.
type ListPromptsOut struct {
	Prompts []*PromptDTO `json:"prompts"`
}

// GetPromptIn is the input for get_prompt.
type GetPromptIn struct {
	PromptID string `json:"prompt_id" jsonschema:"required"`
}

// GetPromptOut is the output for get_prompt.
type GetPromptOut struct {
	Prompt *PromptDTO `json:"prompt"`
}

// CreatePromptIn is the input for create_prompt.
type CreatePromptIn struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
}

// CreatePromptOut is the output for create_prompt.
type CreatePromptOut struct {
	Prompt *PromptDTO `json:"prompt"`
}

// UpdatePromptIn is the input for update_prompt.
type UpdatePromptIn struct {
	PromptID    string `json:"prompt_id" jsonschema:"required"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
}

// UpdatePromptOut is the output for update_prompt.
type UpdatePromptOut struct {
	Prompt *PromptDTO `json:"prompt"`
}

// DeletePromptIn is the input for delete_prompt.
type DeletePromptIn struct {
	PromptID string `json:"prompt_id" jsonschema:"required"`
}

// --- Skill tool structs ---

// ListSkillsOut is the output for list_skills.
type ListSkillsOut struct {
	Skills []*SkillDTO `json:"skills"`
}

// GetSkillIn is the input for get_skill.
type GetSkillIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
}

// GetSkillOut is the output for get_skill.
type GetSkillOut struct {
	Skill *SkillDTO `json:"skill"`
}

// CreateSkillIn is the input for create_skill.
type CreateSkillIn struct {
	Name          string            `json:"name,omitempty"`
	Description   string            `json:"description,omitempty"`
	Files         []SkillFileDTO    `json:"files,omitempty"`
	Content       string            `json:"content,omitempty"`
	Path          string            `json:"path,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
}

// CreateSkillOut is the output for create_skill.
type CreateSkillOut struct {
	Skill *SkillDTO `json:"skill"`
}

// UpdateSkillIn is the input for update_skill.
type UpdateSkillIn struct {
	SkillID       string            `json:"skill_id" jsonschema:"required"`
	Name          string            `json:"name,omitempty"`
	Description   string            `json:"description,omitempty"`
	Files         []SkillFileDTO    `json:"files,omitempty"`
	Content       string            `json:"content,omitempty"`
	Path          string            `json:"path,omitempty"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
}

// UpdateSkillOut is the output for update_skill.
type UpdateSkillOut struct {
	Skill *SkillDTO `json:"skill"`
}

// DeleteSkillIn is the input for delete_skill.
type DeleteSkillIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
}

// ListSkillFilesIn is the input for list_skill_files.
type ListSkillFilesIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
}

// ListSkillFilesOut is the output for list_skill_files.
type ListSkillFilesOut struct {
	Files []SkillFileDTO `json:"files"`
}

// PutSkillFileIn is the input for put_skill_file.
type PutSkillFileIn struct {
	SkillID  string `json:"skill_id" jsonschema:"required"`
	Path     string `json:"path" jsonschema:"required"`
	Dir      bool   `json:"dir,omitempty"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// RenameSkillFileIn is the input for rename_skill_file.
type RenameSkillFileIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
	OldPath string `json:"old_path" jsonschema:"required"`
	NewPath string `json:"new_path" jsonschema:"required"`
}

// DeleteSkillFileIn is the input for delete_skill_file.
type DeleteSkillFileIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
	Path    string `json:"path" jsonschema:"required"`
}

// ImportSkillFromPathIn is the input for import_skill_from_path.
type ImportSkillFromPathIn struct {
	Path string `json:"path" jsonschema:"required"`
}

// PublishSkillIn is the input for publish_skill.
type PublishSkillIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
	Force   bool   `json:"force,omitempty"`
}

// UnpublishSkillIn is the input for unpublish_skill.
type UnpublishSkillIn struct {
	SkillID string `json:"skill_id" jsonschema:"required"`
}

// UpdateSettingsIn is the input for update_settings. Keys are merged into the
// stored settings; omitted keys are left alone.
type UpdateSettingsIn struct {
	Settings map[string]string `json:"settings" jsonschema:"required"`
}

// SettingsOut is the output for get_settings and update_settings.
type SettingsOut struct {
	Settings map[string]string `json:"settings"`
}

// ValidateSkillPathIn is the input for validate_skill_path.
type ValidateSkillPathIn struct {
	Path string `json:"path" jsonschema:"required"`
}

// ValidateSkillPathOut is the output for validate_skill_path.
type ValidateSkillPathOut struct {
	Valid      bool                     `json:"valid"`
	SkillRoot  string                   `json:"skill_root,omitempty"`
	Preview    *SkillDTO                `json:"preview,omitempty"`
	Validation []domain.ValidationIssue `json:"validation,omitempty"`
}

// InspectSkillIn is the input for inspect_skill.
type InspectSkillIn struct {
	Path string `json:"path" jsonschema:"required"`
}

// InspectSkillOut is the output for inspect_skill.
type InspectSkillOut struct {
	SkillRoot  string                   `json:"skill_root"`
	Document   SkillDocumentDTO         `json:"document"`
	Resources  []string                 `json:"resources,omitempty"`
	Validation []domain.ValidationIssue `json:"validation,omitempty"`
}

// SkillDocumentDTO is parsed SKILL.md content for inspection.
type SkillDocumentDTO struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	License       string            `json:"license,omitempty"`
	Compatibility string            `json:"compatibility,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	AllowedTools  string            `json:"allowed_tools,omitempty"`
	Body          string            `json:"body"`
}

// --- MCP Server tool structs ---

// ListMCPServersOut is the output for list_mcp_servers.
type ListMCPServersOut struct {
	MCPServers []*MCPServerDTO `json:"mcp_servers"`
}

// GetMCPServerIn is the input for get_mcp_server.
type GetMCPServerIn struct {
	MCPServerID string `json:"mcp_server_id" jsonschema:"required"`
}

// GetMCPServerOut is the output for get_mcp_server.
type GetMCPServerOut struct {
	MCPServer *MCPServerDTO `json:"mcp_server"`
}

// CreateMCPServerIn is the input for create_mcp_server.
type CreateMCPServerIn struct {
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport" jsonschema:"required"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// CreateMCPServerOut is the output for create_mcp_server.
type CreateMCPServerOut struct {
	MCPServer *MCPServerDTO `json:"mcp_server"`
}

// UpdateMCPServerIn is the input for update_mcp_server.
type UpdateMCPServerIn struct {
	MCPServerID string            `json:"mcp_server_id" jsonschema:"required"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// UpdateMCPServerOut is the output for update_mcp_server.
type UpdateMCPServerOut struct {
	MCPServer *MCPServerDTO `json:"mcp_server"`
}

// DeleteMCPServerIn is the input for delete_mcp_server.
type DeleteMCPServerIn struct {
	MCPServerID string `json:"mcp_server_id" jsonschema:"required"`
}

// TestMCPServerIn is the input for test_mcp_server.
type TestMCPServerIn struct {
	MCPServerID string            `json:"mcp_server_id,omitempty"`
	Persist     bool              `json:"persist,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Transport   string            `json:"transport,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// MCPProbeResultDTO is the probe outcome returned by test_mcp_server.
type MCPProbeResultDTO struct {
	Status        string   `json:"status"`
	Error         string   `json:"error,omitempty"`
	ToolCount     int      `json:"tool_count"`
	ResourceCount int      `json:"resource_count"`
	PromptCount   int      `json:"prompt_count"`
	ToolNames     []string `json:"tool_names,omitempty"`
	ServerName    string   `json:"server_name,omitempty"`
	ServerVersion string   `json:"server_version,omitempty"`
}

// TestMCPServerOut is the output for test_mcp_server.
type TestMCPServerOut struct {
	Probe *MCPProbeResultDTO `json:"probe"`
}

// ImportMCPServersIn is the input for import_mcp_servers.
type ImportMCPServersIn struct {
	Content     string `json:"content" jsonschema:"required"`
	OnDuplicate string `json:"on_duplicate,omitempty"`
}

// ImportMCPServersOut is the output for import_mcp_servers.
type ImportMCPServersOut struct {
	MCPServers []*MCPServerDTO `json:"mcp_servers"`
}

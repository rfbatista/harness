package httpapi

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/adapter/in/mcp"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Services are the driving ports the HTTP API serves. Any may be nil when that
// part of the application is not configured — pass an untyped nil, since a nil
// pointer inside an interface is not nil.
type Services struct {
	Projects ports.Projects
	// Discovery finds git checkouts on disk; nil answers 503.
	Discovery ports.RepositoryDiscovery
	// Env keeps repositories' env files; nil answers 503.
	Env ports.RepositoryEnv
	// RunCommands keeps repositories' saved run commands; nil answers 503.
	RunCommands ports.RepositoryRunCommands
	// Apps runs applications from session worktrees; nil answers 503.
	Apps         ports.AppRunner
	Architecture ports.Architecture
	Agents       interface {
		ports.AgentCatalog
		ports.PromptCatalog
	}
	Capabilities ports.Capabilities
	Settings     ports.SettingsEditor
	Tools        ports.ToolRegistry
	Tasks        ports.TaskRunner
	Sessions     ports.Orchestration
	Planning     ports.Planning
	Workspaces   ports.WorkspaceManager
	// Artifacts records and serves what sessions publish; nil answers 503.
	Artifacts ports.Artifacts
	// TaskChannel is the person's side of the architect channel: task
	// messages, review requests, status checks. Nil answers 503.
	TaskChannel ports.TaskChannelUI
}

// Handler serves the application's driving ports as HTTP endpoints (the same
// contract as the MCP tools). It holds ports, not services, so a test can hand
// it any implementation.
type Handler struct {
	projects     ports.Projects
	discovery    ports.RepositoryDiscovery
	env          ports.RepositoryEnv
	runCommands  ports.RepositoryRunCommands
	apps         ports.AppRunner
	architecture ports.Architecture
	agents       interface {
		ports.AgentCatalog
		ports.PromptCatalog
	}
	capabilities  ports.Capabilities
	settings      ports.SettingsEditor
	toolingSvc    ports.ToolRegistry
	execSvc       ports.TaskRunner
	orchSvc       ports.Orchestration
	planningSvc   ports.Planning
	workspacesSvc ports.WorkspaceManager
	artifactsSvc  ports.Artifacts
	channel       ports.TaskChannelUI
}

// NewHandler returns an HTTP handler that serves /api/list_tree, /api/list_zones, /api/list_projects, etc.
func NewHandler(s Services) *Handler {
	return &Handler{
		projects:      s.Projects,
		discovery:     s.Discovery,
		env:           s.Env,
		runCommands:   s.RunCommands,
		apps:          s.Apps,
		architecture:  s.Architecture,
		agents:        s.Agents,
		capabilities:  s.Capabilities,
		settings:      s.Settings,
		toolingSvc:    s.Tools,
		execSvc:       s.Tasks,
		orchSvc:       s.Sessions,
		planningSvc:   s.Planning,
		workspacesSvc: s.Workspaces,
		artifactsSvc:  s.Artifacts,
		channel:       s.TaskChannel,
	}
}

// handleHealthCheck godoc
// @Summary Health Check
// @Description Returns the health status of the service
// @Tags health
// @Success 200 {string} string "{\"status\":\"ok\"}"
// @Router /health [get]
func (h *Handler) handleHealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleListTools(c echo.Context) error {
	var all []map[string]any
	for _, t := range h.toolingSvc.List() {
		all = append(all, toolToMap(t.Name, t.Description, t.InputSchema, t.Source, t.ID))
	}
	for _, t := range h.capabilities.ListTools() {
		all = append(all, toolToMap(t.Name, t.Description, t.InputSchema, t.Source, t.ID))
	}
	return c.JSON(http.StatusOK, map[string]any{"tools": all})
}

func toolToMap(name, description string, inputSchema map[string]any, source, id string) map[string]any {
	m := map[string]any{
		"name":        name,
		"description": description,
		"source":      source,
	}
	if id != "" {
		m["id"] = id
	}
	if len(inputSchema) > 0 {
		m["input_schema"] = inputSchema
	}
	return m
}

func (h *Handler) handleListProjects(c echo.Context) error {
	projects, err := h.projects.ListProjects(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ListProjectsOut{Projects: mcp.ProjectsToDTO(projects)})
}

func (h *Handler) handleGetProject(c echo.Context) error {
	p, err := h.projects.GetProject(c.Request().Context(), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.GetProjectOut{Project: mcp.ProjectToDTO(p)})
}

func (h *Handler) handleCreateProject(c echo.Context) error {
	var in mcp.CreateProjectIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.projects.CreateProject(c.Request().Context(), in.Name, in.RootDir)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateProjectOut{Project: mcp.ProjectToDTO(p)})
}

func (h *Handler) handleUpdateProject(c echo.Context) error {
	var in mcp.UpdateProjectIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.projects.UpdateProject(c.Request().Context(), in.ProjectID, in.Name, in.RootDir)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateProjectOut{Project: mcp.ProjectToDTO(p)})
}

func (h *Handler) handleDeleteProject(c echo.Context) error {
	var in mcp.DeleteProjectIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.projects.DeleteProject(c.Request().Context(), in.ProjectID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleAddIgnoredPath(c echo.Context) error {
	var in mcp.AddIgnoredPathIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.projects.AddIgnoredPath(c.Request().Context(), in.ProjectID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.AddIgnoredPathOut{Project: mcp.ProjectToDTO(p)})
}

func (h *Handler) handleRemoveIgnoredPath(c echo.Context) error {
	var in mcp.RemoveIgnoredPathIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.projects.RemoveIgnoredPath(c.Request().Context(), in.ProjectID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.RemoveIgnoredPathOut{Project: mcp.ProjectToDTO(p)})
}

func (h *Handler) handleListTree(c echo.Context) error {
	tree, err := h.architecture.ListTree(c.QueryParam("root"), c.QueryParam("project_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ListTreeOut{Tree: mcp.TreeNodeToDTO(tree)})
}

func (h *Handler) handleListZones(c echo.Context) error {
	projectID := c.QueryParam("project_id")
	if projectID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "project_id is required")
	}
	zones := h.architecture.ListZones(projectID)
	return c.JSON(http.StatusOK, mcp.ListZonesOut{Zones: mcp.ZonesToDTO(zones)})
}

func (h *Handler) handleListMatchingPaths(c echo.Context) error {
	paths, err := h.architecture.ListMatchingPaths(c.QueryParam("root"), c.QueryParam("project_id"), c.QueryParam("pattern"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ListMatchingPathsOut{Paths: paths})
}

func (h *Handler) handleGetZone(c echo.Context) error {
	z := h.architecture.GetZone(c.QueryParam("zone_id"))
	if z == nil {
		return echo.NewHTTPError(http.StatusNotFound, "zone not found")
	}
	return c.JSON(http.StatusOK, mcp.GetZoneOut{Zone: mcp.ZoneToDTO(z)})
}

func (h *Handler) handleCreateZone(c echo.Context) error {
	var in mcp.CreateZoneIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	z, err := h.architecture.CreateZone(in.ProjectID, in.Name, in.Pattern, in.Purpose, mcp.DTOToRules(in.Rules), mcp.DTOToAgents(in.AssignedAgents))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateZoneOut{Zone: mcp.ZoneToDTO(z)})
}

func (h *Handler) handleUpdateZone(c echo.Context) error {
	var in mcp.UpdateZoneIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	z, err := h.architecture.UpdateZone(in.ZoneID, in.Name, in.Pattern, in.Purpose, mcp.DTOToRules(in.Rules), mcp.DTOToAgents(in.AssignedAgents))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateZoneOut{Zone: mcp.ZoneToDTO(z)})
}

func (h *Handler) handleAssignPathToZone(c echo.Context) error {
	var in mcp.AssignPathToZoneIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	z, err := h.architecture.AssignPathToZone(in.ZoneID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.AssignPathToZoneOut{Zone: mcp.ZoneToDTO(z)})
}

func (h *Handler) handleRemovePathFromZone(c echo.Context) error {
	var in mcp.RemovePathFromZoneIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	z, err := h.architecture.UnassignPathFromZone(in.ZoneID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.RemovePathFromZoneOut{Zone: mcp.ZoneToDTO(z)})
}

// --- Agent handlers ---

func (h *Handler) handleListAgents(c echo.Context) error {
	agents, err := h.agents.ListAgents(c.Request().Context())
	if err != nil {
		return err
	}
	out := make([]*mcp.AgentDTO, len(agents))
	for i, a := range agents {
		out[i] = mcp.AgentToDTO(a)
	}
	return c.JSON(http.StatusOK, mcp.ListAgentsOut{Agents: out})
}

func (h *Handler) handleGetAgent(c echo.Context) error {
	a, err := h.agents.GetAgent(c.Request().Context(), c.QueryParam("agent_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.GetAgentOut{Agent: mcp.AgentToDTO(a)})
}

func (h *Handler) handleCreateAgent(c echo.Context) error {
	var in mcp.CreateAgentIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	a, err := h.agents.CreateAgent(c.Request().Context(), in.Name, in.Description, in.PromptID, in.SkillIDs, in.MCPServerIDs)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateAgentOut{Agent: mcp.AgentToDTO(a)})
}

func (h *Handler) handleUpdateAgent(c echo.Context) error {
	var in mcp.UpdateAgentIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	a, err := h.agents.UpdateAgent(c.Request().Context(), in.AgentID, in.Name, in.Description, in.PromptID, in.SkillIDs, in.MCPServerIDs)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateAgentOut{Agent: mcp.AgentToDTO(a)})
}

func (h *Handler) handleDeleteAgent(c echo.Context) error {
	var in mcp.DeleteAgentIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.agents.DeleteAgent(c.Request().Context(), in.AgentID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Prompt handlers ---

func (h *Handler) handleListPrompts(c echo.Context) error {
	prompts := h.agents.ListPrompts()
	return c.JSON(http.StatusOK, mcp.ListPromptsOut{Prompts: mcp.PromptsToDTO(prompts)})
}

func (h *Handler) handleGetPrompt(c echo.Context) error {
	p := h.agents.GetPrompt(c.QueryParam("prompt_id"))
	if p == nil {
		return echo.NewHTTPError(http.StatusNotFound, "prompt not found")
	}
	return c.JSON(http.StatusOK, mcp.GetPromptOut{Prompt: mcp.PromptToDTO(p)})
}

func (h *Handler) handleCreatePrompt(c echo.Context) error {
	var in mcp.CreatePromptIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.agents.CreatePrompt(in.Name, in.Description, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreatePromptOut{Prompt: mcp.PromptToDTO(p)})
}

func (h *Handler) handleUpdatePrompt(c echo.Context) error {
	var in mcp.UpdatePromptIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	p, err := h.agents.UpdatePrompt(in.PromptID, in.Name, in.Description, in.Content)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdatePromptOut{Prompt: mcp.PromptToDTO(p)})
}

func (h *Handler) handleDeletePrompt(c echo.Context) error {
	var in mcp.DeletePromptIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.agents.DeletePrompt(in.PromptID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Skill handlers ---

func (h *Handler) handleListSkills(c echo.Context) error {
	skills := h.capabilities.ListSkills()
	return c.JSON(http.StatusOK, mcp.ListSkillsOut{Skills: mcp.SkillsToDTO(skills)})
}

func (h *Handler) handleGetSkill(c echo.Context) error {
	skill := h.capabilities.GetSkill(c.QueryParam("skill_id"))
	if skill == nil {
		return echo.NewHTTPError(http.StatusNotFound, "skill not found")
	}
	return c.JSON(http.StatusOK, mcp.GetSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleCreateSkill(c echo.Context) error {
	var in mcp.CreateSkillIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.CreateSkill(skillInputFromHTTP(in))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func skillInputFromHTTP(in mcp.CreateSkillIn) domain.SkillInput {
	return domain.SkillInput{
		Name:          in.Name,
		Description:   in.Description,
		Files:         mcp.SkillFilesFromDTO(in.Files),
		Content:       in.Content,
		Path:          in.Path,
		License:       in.License,
		Compatibility: in.Compatibility,
		Metadata:      in.Metadata,
		AllowedTools:  in.AllowedTools,
	}
}

func skillInputFromUpdateHTTP(in mcp.UpdateSkillIn) domain.SkillInput {
	return domain.SkillInput{
		Name:          in.Name,
		Description:   in.Description,
		Files:         mcp.SkillFilesFromDTO(in.Files),
		Content:       in.Content,
		Path:          in.Path,
		License:       in.License,
		Compatibility: in.Compatibility,
		Metadata:      in.Metadata,
		AllowedTools:  in.AllowedTools,
	}
}

func (h *Handler) handleUpdateSkill(c echo.Context) error {
	var in mcp.UpdateSkillIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.UpdateSkill(in.SkillID, skillInputFromUpdateHTTP(in))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleDeleteSkill(c echo.Context) error {
	var in mcp.DeleteSkillIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.capabilities.DeleteSkill(in.SkillID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleValidateSkillPath(c echo.Context) error {
	result, err := h.capabilities.ValidateSkillPath(c.QueryParam("path"))
	if err != nil {
		return err
	}
	out := mcp.ValidateSkillPathOut{
		Valid:      result.Valid,
		SkillRoot:  result.SkillRoot,
		Validation: result.Validation,
	}
	if result.Preview != nil {
		out.Preview = mcp.SkillToDTO(result.Preview)
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) handleInspectSkill(c echo.Context) error {
	result, err := h.capabilities.InspectSkill(c.QueryParam("path"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.InspectSkillOut{
		SkillRoot: result.SkillRoot,
		Document: mcp.SkillDocumentDTO{
			Name:          result.Document.Name,
			Description:   result.Document.Description,
			License:       result.Document.License,
			Compatibility: result.Document.Compatibility,
			Metadata:      result.Document.Metadata,
			AllowedTools:  result.Document.AllowedTools,
			Body:          result.Document.Body,
		},
		Resources:  result.Resources,
		Validation: result.Validation,
	})
}

func (h *Handler) handleListSkillFiles(c echo.Context) error {
	files, err := h.capabilities.ListSkillFiles(c.QueryParam("skill_id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ListSkillFilesOut{Files: mcp.SkillFilesToDTO(files)})
}

func (h *Handler) handlePutSkillFile(c echo.Context) error {
	var in mcp.PutSkillFileIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.PutSkillFile(in.SkillID, domain.SkillFile{
		Path:     in.Path,
		Dir:      in.Dir,
		Content:  in.Content,
		Encoding: in.Encoding,
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleRenameSkillFile(c echo.Context) error {
	var in mcp.RenameSkillFileIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.RenameSkillFile(in.SkillID, in.OldPath, in.NewPath)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleDeleteSkillFile(c echo.Context) error {
	var in mcp.DeleteSkillFileIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.DeleteSkillFile(in.SkillID, in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleImportSkillFromPath(c echo.Context) error {
	var in mcp.ImportSkillFromPathIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.ImportSkillFromPath(in.Path)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handlePublishSkill(c echo.Context) error {
	var in mcp.PublishSkillIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.PublishSkill(in.SkillID, in.Force)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleUnpublishSkill(c echo.Context) error {
	var in mcp.UnpublishSkillIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	skill, err := h.capabilities.UnpublishSkill(in.SkillID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateSkillOut{Skill: mcp.SkillToDTO(skill)})
}

func (h *Handler) handleGetSettings(c echo.Context) error {
	settings, err := h.settings.GetSettings()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.SettingsOut{Settings: settings})
}

func (h *Handler) handleUpdateSettings(c echo.Context) error {
	var in mcp.UpdateSettingsIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	settings, err := h.settings.UpdateSettings(in.Settings)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.SettingsOut{Settings: settings})
}

// --- MCP Server handlers ---

func (h *Handler) handleListMCPServers(c echo.Context) error {
	servers := h.capabilities.ListMCPServers()
	return c.JSON(http.StatusOK, mcp.ListMCPServersOut{MCPServers: mcp.MCPServersToDTO(servers)})
}

func (h *Handler) handleGetMCPServer(c echo.Context) error {
	m := h.capabilities.GetMCPServer(c.QueryParam("mcp_server_id"))
	if m == nil {
		return echo.NewHTTPError(http.StatusNotFound, "mcp server not found")
	}
	return c.JSON(http.StatusOK, mcp.GetMCPServerOut{MCPServer: mcp.MCPServerToDTO(m)})
}

func (h *Handler) handleCreateMCPServer(c echo.Context) error {
	var in mcp.CreateMCPServerIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	m, err := h.capabilities.CreateMCPServer(mcpServerInputFromHTTP(in.Name, in.Description, in.Transport, in.Command, in.Args, in.URL, in.Env, in.Headers))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.CreateMCPServerOut{MCPServer: mcp.MCPServerToDTO(m)})
}

func (h *Handler) handleUpdateMCPServer(c echo.Context) error {
	var in mcp.UpdateMCPServerIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	m, err := h.capabilities.UpdateMCPServer(in.MCPServerID, mcpServerInputFromHTTP(in.Name, in.Description, in.Transport, in.Command, in.Args, in.URL, in.Env, in.Headers))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.UpdateMCPServerOut{MCPServer: mcp.MCPServerToDTO(m)})
}

func (h *Handler) handleDeleteMCPServer(c echo.Context) error {
	var in mcp.DeleteMCPServerIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.capabilities.DeleteMCPServer(in.MCPServerID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleTestMCPServer(c echo.Context) error {
	var in mcp.TestMCPServerIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	server := domain.MCPServer{
		Name: in.Name, Description: in.Description, Transport: in.Transport,
		Command: in.Command, Args: in.Args, URL: in.URL,
		Env: in.Env, Headers: in.Headers,
	}
	if in.MCPServerID != "" {
		existing := h.capabilities.GetMCPServer(in.MCPServerID)
		if existing == nil {
			return echo.NewHTTPError(http.StatusNotFound, "mcp server not found")
		}
		server = *existing
	}
	result, err := h.capabilities.ProbeMCPServer(c.Request().Context(), server)
	if err != nil {
		return err
	}
	if in.MCPServerID != "" && in.Persist {
		if _, err := h.capabilities.UpdateMCPServerProbeResult(in.MCPServerID, result); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, mcp.TestMCPServerOut{Probe: mcp.ProbeResultToDTO(result)})
}

func (h *Handler) handleImportMCPServers(c echo.Context) error {
	var in mcp.ImportMCPServersIn
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	servers, err := h.capabilities.ImportMCPServers(in.Content, in.OnDuplicate)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, mcp.ImportMCPServersOut{MCPServers: mcp.MCPServersToDTO(servers)})
}

// --- Tool handlers ---

func (h *Handler) handleGetTool(c echo.Context) error {
	id := c.QueryParam("id")
	if t := h.toolingSvc.GetByID(id); t != nil {
		return c.JSON(http.StatusOK, map[string]any{"tool": toolToMap(t.Name, t.Description, t.InputSchema, t.Source, t.ID)})
	}
	if t := h.toolingSvc.Get(id); t != nil {
		return c.JSON(http.StatusOK, map[string]any{"tool": toolToMap(t.Name, t.Description, t.InputSchema, t.Source, t.ID)})
	}
	if t := h.capabilities.GetTool(id); t != nil {
		return c.JSON(http.StatusOK, map[string]any{"tool": toolToMap(t.Name, t.Description, t.InputSchema, t.Source, t.ID)})
	}
	return echo.NewHTTPError(http.StatusNotFound, "tool not found")
}

func (h *Handler) handleCreateTool(c echo.Context) error {
	var in struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"input_schema"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	t, err := h.capabilities.CreateTool(in.Name, in.Description, in.InputSchema)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"tool": t})
}

func (h *Handler) handleUpdateTool(c echo.Context) error {
	var in struct {
		ID          string         `json:"id"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		InputSchema map[string]any `json:"input_schema"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	t, err := h.capabilities.UpdateTool(in.ID, in.Name, in.Description, in.InputSchema)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"tool": t})
}

func (h *Handler) handleDeleteTool(c echo.Context) error {
	var in struct {
		ID string `json:"id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if err := h.capabilities.DeleteTool(in.ID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Task handlers ---

func (h *Handler) handleCreateTask(c echo.Context) error {
	if h.execSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "execution service not configured (set GEMINI_API_KEY)")
	}
	var req ports.RunTaskRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.ProjectID == "" {
		req.ProjectID = c.Request().Header.Get("X-Project-ID")
	}
	task, err := h.execSvc.RunTask(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, map[string]any{"task": task})
}

func (h *Handler) handleListTasks(c echo.Context) error {
	if h.execSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "execution service not configured (set GEMINI_API_KEY)")
	}
	filter := ports.TaskFilter{
		ProjectID: c.QueryParam("project_id"),
		ZoneID:    c.QueryParam("zone_id"),
		Status:    domain.TaskStatus(c.QueryParam("status")),
	}
	if filter.ProjectID == "" {
		filter.ProjectID = c.Request().Header.Get("X-Project-ID")
	}
	return c.JSON(http.StatusOK, map[string]any{"tasks": h.execSvc.ListTasks(filter)})
}

func (h *Handler) handleTaskByID(c echo.Context) error {
	if h.execSvc == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "execution service not configured (set GEMINI_API_KEY)")
	}
	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "task id required")
	}
	task := h.execSvc.GetTask(id)
	if task == nil {
		return echo.NewHTTPError(http.StatusNotFound, "task not found")
	}
	return c.JSON(http.StatusOK, map[string]any{"task": task})
}

// mcpServerInputFromHTTP builds the service input from the flat HTTP DTO.
func mcpServerInputFromHTTP(name, description, transport, command string, args []string, url string, env, headers map[string]string) domain.MCPServerInput {
	return domain.MCPServerInput{
		Name: name, Description: description, Transport: transport,
		Command: command, Args: args, URL: url, Env: env, Headers: headers,
	}
}

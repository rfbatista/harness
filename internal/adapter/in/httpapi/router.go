package httpapi

import (
	"github.com/labstack/echo/v4"
)

// NewRouter builds an *echo.Echo serving the JSON API under /api. It installs a
// custom error handler mapping domain errors to the {"error": msg} contract.
// Read endpoints are GET-only (inputs via query params); write endpoints are
// POST-only (inputs via JSON body). Each route maps to exactly one handler per
// method.
func NewRouter(h *Handler, opts ...Option) *echo.Echo {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = errorHandler

	g := e.Group("/api")
	if o.token != "" {
		g.Use(requireToken(o.token))
	}

	g.GET("/health", h.handleHealthCheck)
	g.GET("/tools", h.handleListTools)

	// projects
	g.GET("/list_projects", h.handleListProjects)
	g.GET("/get_project", h.handleGetProject)
	g.POST("/create_project", h.handleCreateProject)
	g.POST("/update_project", h.handleUpdateProject)
	g.POST("/delete_project", h.handleDeleteProject)
	g.POST("/add_ignored_path", h.handleAddIgnoredPath)
	g.POST("/remove_ignored_path", h.handleRemoveIgnoredPath)

	// repositories
	g.GET("/list_repositories", h.handleListRepositories)
	g.GET("/find_repositories", h.handleFindRepositories) // git checkouts inside a directory, to choose from
	g.GET("/list_repository_env_files", h.handleListEnvFiles)
	g.POST("/save_repository_env_file", h.handleSaveEnvFile)
	g.POST("/delete_repository_env_file", h.handleDeleteEnvFile)
	g.POST("/import_repository_env_file", h.handleImportEnvFile)
	g.GET("/get_repository", h.handleGetRepository)
	g.POST("/create_repository", h.handleCreateRepository)
	g.POST("/update_repository", h.handleUpdateRepository)
	g.POST("/delete_repository", h.handleDeleteRepository)
	g.POST("/add_repository_ignored_path", h.handleAddRepositoryIgnoredPath)
	g.POST("/remove_repository_ignored_path", h.handleRemoveRepositoryIgnoredPath)

	// workspaces
	g.GET("/list_workspaces", h.handleListWorkspaces)
	g.GET("/list_branches", h.handleListBranches)
	g.POST("/create_workspace", h.handleCreateWorkspace)
	g.POST("/delete_workspace", h.handleDeleteWorkspace)

	// bounded contexts
	g.GET("/list_bounded_contexts", h.handleListBoundedContexts)
	g.GET("/get_bounded_context", h.handleGetBoundedContext)
	g.POST("/create_bounded_context", h.handleCreateBoundedContext)
	g.POST("/update_bounded_context", h.handleUpdateBoundedContext)
	g.POST("/delete_bounded_context", h.handleDeleteBoundedContext)
	g.POST("/assign_zone_to_bounded_context", h.handleAssignZoneToBoundedContext)
	g.POST("/unassign_zone_from_bounded_context", h.handleUnassignZoneFromBoundedContext)

	// zones / tree
	g.GET("/list_tree", h.handleListTree)
	g.GET("/list_zones", h.handleListZones)
	g.GET("/list_matching_paths", h.handleListMatchingPaths)
	g.GET("/get_zone", h.handleGetZone)
	g.POST("/create_zone", h.handleCreateZone)
	g.POST("/update_zone", h.handleUpdateZone)
	g.POST("/assign_path_to_zone", h.handleAssignPathToZone)
	g.POST("/remove_path_from_zone", h.handleRemovePathFromZone)

	// agents
	g.GET("/list_agents", h.handleListAgents)
	g.GET("/get_agent", h.handleGetAgent)
	g.POST("/create_agent", h.handleCreateAgent)
	g.POST("/update_agent", h.handleUpdateAgent)
	g.POST("/delete_agent", h.handleDeleteAgent)

	// prompts
	g.GET("/list_prompts", h.handleListPrompts)
	g.GET("/get_prompt", h.handleGetPrompt)
	g.POST("/create_prompt", h.handleCreatePrompt)
	g.POST("/update_prompt", h.handleUpdatePrompt)
	g.POST("/delete_prompt", h.handleDeletePrompt)

	// skills
	g.GET("/list_skills", h.handleListSkills)
	g.GET("/get_skill", h.handleGetSkill)
	g.POST("/create_skill", h.handleCreateSkill)
	g.POST("/update_skill", h.handleUpdateSkill)
	g.POST("/delete_skill", h.handleDeleteSkill)
	g.GET("/list_skill_files", h.handleListSkillFiles)
	g.POST("/put_skill_file", h.handlePutSkillFile)
	g.POST("/rename_skill_file", h.handleRenameSkillFile)
	g.POST("/delete_skill_file", h.handleDeleteSkillFile)
	g.POST("/import_skill_from_path", h.handleImportSkillFromPath)
	g.GET("/validate_skill_path", h.handleValidateSkillPath)
	g.GET("/inspect_skill", h.handleInspectSkill)
	g.POST("/publish_skill", h.handlePublishSkill)
	g.POST("/unpublish_skill", h.handleUnpublishSkill)

	// settings
	g.GET("/get_settings", h.handleGetSettings)
	g.POST("/update_settings", h.handleUpdateSettings)

	// mcp servers
	g.GET("/list_mcp_servers", h.handleListMCPServers)
	g.GET("/get_mcp_server", h.handleGetMCPServer)
	g.POST("/create_mcp_server", h.handleCreateMCPServer)
	g.POST("/update_mcp_server", h.handleUpdateMCPServer)
	g.POST("/delete_mcp_server", h.handleDeleteMCPServer)
	g.POST("/test_mcp_server", h.handleTestMCPServer)
	g.POST("/import_mcp_servers", h.handleImportMCPServers)

	// tools
	g.GET("/get_tool", h.handleGetTool)
	g.POST("/create_tool", h.handleCreateTool)
	g.POST("/update_tool", h.handleUpdateTool)
	g.POST("/delete_tool", h.handleDeleteTool)

	// tasks
	g.GET("/tasks", h.handleListTasks)
	g.POST("/tasks", h.handleCreateTask)
	g.GET("/tasks/:id", h.handleTaskByID)

	// sessions
	g.GET("/sessions", h.handleListSessions)
	g.POST("/sessions", h.handleCreateSession)
	g.GET("/sessions/:id", h.handleGetSession)
	g.DELETE("/sessions/:id", h.handleDeleteSession)
	g.GET("/events", h.handleEvents) // a project's session changes (SSE)
	g.GET("/sessions/:id/events", h.handleSessionEvents)
	g.GET("/sessions/:id/terminal", h.handleSessionTerminal) // WebSocket upgrade
	g.GET("/sessions/:id/history", h.handleSessionHistory)
	g.POST("/sessions/:id/messages", h.handleSessionMessages)
	g.POST("/sessions/:id/approvals/:approvalID", h.handleSessionApproval)
	g.POST("/sessions/:id/auto-run", h.handleSessionAutoRun)
	g.POST("/sessions/:id/stop", h.handleSessionStop)

	// interactive sessions: claude runs in the client's terminal (tui-client).
	// Client plumbing with no MCP twin — an agent has no terminal to hand out.
	g.POST("/start_interactive_session", h.handleStartInteractiveSession)
	g.POST("/resume_interactive_session", h.handleResumeInteractiveSession)
	g.POST("/end_interactive_session", h.handleEndInteractiveSession)
	g.POST("/interactive_session_started", h.handleInteractiveSessionStarted)

	// tickets & documents
	g.GET("/list_tickets", h.handleListTickets)
	g.GET("/get_ticket", h.handleGetTicket)
	g.POST("/create_ticket", h.handleCreateTicket)
	g.POST("/update_ticket", h.handleUpdateTicket)
	g.POST("/delete_ticket", h.handleDeleteTicket)
	g.GET("/list_documents", h.handleListDocuments)
	g.GET("/get_document", h.handleGetDocument)
	g.POST("/create_document", h.handleCreateDocument)
	g.POST("/update_document", h.handleUpdateDocument)
	g.POST("/delete_document", h.handleDeleteDocument)
	g.POST("/link_document_to_ticket", h.handleLinkDocument)
	g.POST("/unlink_document_from_ticket", h.handleUnlinkDocument)
	g.GET("/list_ticket_documents", h.handleListTicketDocuments)

	return e
}

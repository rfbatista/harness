package httpapi

import (
	"net/http"
	"strings"

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
	e.Use(reportRoute)

	g := e.Group("/api")
	if o.token != "" {
		g.Use(requireToken(o.token))
	}

	g.GET("/health", h.handleHealthCheck)

	// a browser tab's one live stream, carrying the SSE feeds below it follows
	g.GET("/stream", h.handleStream)
	g.POST("/stream/follow", h.handleStreamFollow)
	g.POST("/stream/unfollow", h.handleStreamUnfollow)
	g.GET("/tools", h.handleListTools)

	// projects
	g.GET("/list_projects", h.handleListProjects)
	g.GET("/list_project_summaries", h.handleListProjectSummaries)
	g.GET("/project_events", h.handleProjectEvents) // the set of projects as it changes (SSE)
	g.GET("/get_project", h.handleGetProject)
	g.POST("/create_project", h.handleCreateProject)
	g.POST("/update_project", h.handleUpdateProject)
	g.POST("/delete_project", h.handleDeleteProject)
	g.POST("/add_ignored_path", h.handleAddIgnoredPath)
	g.POST("/remove_ignored_path", h.handleRemoveIgnoredPath)
	g.POST("/set_project_agents_read_all_repos", h.handleSetProjectAgentsReadAllRepos)

	// repositories
	g.GET("/list_repositories", h.handleListRepositories)
	g.GET("/find_repositories", h.handleFindRepositories) // git checkouts inside a directory, to choose from
	g.GET("/list_repository_env_files", h.handleListEnvFiles)
	g.POST("/save_repository_env_file", h.handleSaveEnvFile)
	g.POST("/delete_repository_env_file", h.handleDeleteEnvFile)
	g.POST("/import_repository_env_file", h.handleImportEnvFile)
	g.GET("/list_repository_run_commands", h.handleListRunCommands)
	g.POST("/save_repository_run_command", h.handleSaveRunCommand)
	g.POST("/delete_repository_run_command", h.handleDeleteRunCommand)

	// token saver activations of a project, repository or task
	g.GET("/list_token_saver_activations", h.handleListTokenSaverActivations)
	g.POST("/set_token_saver_activation", h.handleSetTokenSaverActivation)

	// application runs: a repository's app run from a session's worktree
	g.GET("/list_runs", h.handleListRuns)
	g.GET("/list_run_worktrees", h.handleListRunWorktrees)
	g.POST("/start_run", h.handleStartRun)
	g.POST("/stop_run", h.handleStopRun)
	g.GET("/runs/:id/terminal", h.handleRunTerminal) // WebSocket upgrade
	g.GET("/get_repository", h.handleGetRepository)
	g.POST("/create_repository", h.handleCreateRepository)
	g.POST("/update_repository", h.handleUpdateRepository)
	g.POST("/delete_repository", h.handleDeleteRepository)
	g.POST("/add_repository_ignored_path", h.handleAddRepositoryIgnoredPath)
	g.POST("/remove_repository_ignored_path", h.handleRemoveRepositoryIgnoredPath)

	// workspaces
	g.GET("/list_workspaces", h.handleListWorkspaces)
	g.GET("/list_branches", h.handleListBranches)
	g.GET("/check_branch_name", h.handleCheckBranchName)
	g.POST("/create_workspace", h.handleCreateWorkspace)
	g.POST("/delete_workspace", h.handleDeleteWorkspace)
	// a workspace's branch as a pull request on its origin
	g.GET("/pull_request", h.handlePullRequest)
	g.POST("/create_pull_request", h.handleCreatePullRequest)
	g.GET("/pull_request_bases", h.handlePullRequestBases)
	g.POST("/refresh_pull_request_bases", h.handleRefreshPullRequestBases)

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

	// the writing helper: one step per request, streamed (SSE framing)
	g.POST("/run_writing_helper_step", h.handleRunWritingHelperStep)

	// agent sandbox settings (the Agent isolation page)
	g.GET("/get_sandbox_settings", h.handleGetSandboxSettings)
	g.POST("/set_sandbox_enabled", h.handleSetSandboxEnabled)
	g.POST("/add_sandbox_path", h.handleAddSandboxPath)
	g.POST("/update_sandbox_path", h.handleUpdateSandboxPath)
	g.POST("/remove_sandbox_path", h.handleRemoveSandboxPath)

	// managed claude (the Agent isolation page)
	g.GET("/get_managed_claude", h.handleGetManagedClaude)
	g.GET("/list_managed_claude_releases", h.handleListManagedClaudeReleases)
	g.POST("/install_managed_claude", h.handleInstallManagedClaude)
	g.POST("/switch_managed_claude", h.handleSwitchManagedClaude)
	g.POST("/cancel_managed_claude_install", h.handleCancelManagedClaudeInstall)
	g.POST("/remove_managed_claude_version", h.handleRemoveManagedClaudeVersion)
	// agent credential (the Agent isolation page)
	g.GET("/get_agent_credential", h.handleGetAgentCredential)
	g.POST("/set_agent_credential", h.handleSetAgentCredential)
	g.POST("/remove_agent_credential", h.handleRemoveAgentCredential)
	g.GET("/get_agent_oauth", h.handleGetAgentOAuth)
	g.POST("/start_agent_oauth", h.handleStartAgentOAuth)
	g.POST("/submit_agent_oauth_code", h.handleSubmitAgentOAuthCode)
	g.POST("/cancel_agent_oauth", h.handleCancelAgentOAuth)
	g.GET("/get_agent_auth_status", h.handleGetAgentAuthStatus)
	g.POST("/sign_out_agent_auth", h.handleSignOutAgentAuth)

	// the agent catalog
	g.GET("/get_agent_catalog", h.handleGetAgentCatalog)
	g.GET("/get_catalog_skill", h.handleGetCatalogSkill)
	g.GET("/check_env_vars", h.handleCheckEnvVars)
	g.POST("/save_catalog_agent", h.handleSaveCatalogAgent)
	g.POST("/delete_catalog_agent", h.handleDeleteCatalogAgent)
	g.POST("/save_catalog_bundle", h.handleSaveCatalogBundle)
	g.POST("/delete_catalog_bundle", h.handleDeleteCatalogBundle)
	g.POST("/save_catalog_skill", h.handleSaveCatalogSkill)
	g.POST("/delete_catalog_skill", h.handleDeleteCatalogSkill)
	g.POST("/save_catalog_mcp_server", h.handleSaveCatalogMCPServer)
	g.POST("/delete_catalog_mcp_server", h.handleDeleteCatalogMCPServer)
	g.POST("/test_catalog_mcp_server", h.handleTestCatalogMCPServer)
	g.GET("/get_project_mcp_servers", h.handleGetProjectMCPServers)
	g.POST("/set_project_mcp_servers", h.handleSetProjectMCPServers)
	g.GET("/get_project_agents", h.handleGetProjectAgents)
	g.POST("/set_project_agents", h.handleSetProjectAgents)
	g.POST("/undo_catalog_change", h.handleUndoCatalogChange)

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
	g.GET("/sessions/:id/transcript", h.handleSessionTranscript)
	g.GET("/sessions/:id/sandbox/violations", h.handleSessionSandboxViolations)
	// merging a session's branch into its repository's default branch, in the local checkout
	g.GET("/sessions/:id/merge", h.handleSessionMergeOffer)
	g.POST("/sessions/:id/merge", h.handleSessionMerge)

	// artifacts: what sessions publish for the Design tab. The /view/ routes
	// are loaded as plain subresources (<img>, <video>, <iframe>), under the
	// same token rule as the rest of /api.
	g.GET("/artifacts", h.handleListArtifacts)
	g.GET("/artifacts/:id", h.handleGetArtifact)
	g.DELETE("/artifacts/:id", h.handleDeleteArtifact)
	g.POST("/set_artifact_scope", h.handleSetArtifactScope)
	g.POST("/attach_artifact_to_ticket", h.handleAttachArtifactToTicket)
	g.POST("/detach_artifact_from_ticket", h.handleDetachArtifactFromTicket)
	// GET and HEAD: echo does not fall HEAD back to GET, and curl -I is how headers are checked.
	g.Match([]string{http.MethodGet, http.MethodHead}, "/artifacts/:id/view", h.handleArtifactViewRedirect) // the slash matters: relative refs resolve under it
	g.Match([]string{http.MethodGet, http.MethodHead}, "/artifacts/:id/view/*", h.handleArtifactView)
	g.POST("/sessions/:id/messages", h.handleSessionMessages)
	g.POST("/sessions/:id/approvals/:approvalID", h.handleSessionApproval)
	g.POST("/sessions/:id/auto-run", h.handleSessionAutoRun)
	g.POST("/sessions/:id/stop", h.handleSessionStop)

	// interactive sessions: claude runs in the client's terminal (tui-client).
	// Client plumbing with no MCP twin — an agent has no terminal to hand out.
	g.POST("/start_interactive_session", h.handleStartInteractiveSession)
	g.POST("/resume_interactive_session", h.handleResumeInteractiveSession)
	g.POST("/end_interactive_session", h.handleEndInteractiveSession)
	g.POST("/interactive_session_started", h.handleInteractiveSessionHook)
	g.POST("/interactive_session_hook", h.handleInteractiveSessionHook)
	// the edit proxy: a session's hooks and shims check and commit changes
	// to its virtual shell's files
	g.POST(strings.TrimPrefix(ShellCheckRoute, "/api"), h.handleShellCheck)
	g.POST(strings.TrimPrefix(ShellCommitRoute, "/api"), h.handleShellCommit)
	// the architecture rules: the same sessions' hooks, shims and commits ask
	// whether an action keeps to the repository's layers
	g.POST(strings.TrimPrefix(ArchitectureCheckRoute, "/api"), h.handleArchitectureCheck)
	g.POST(strings.TrimPrefix(ArchitectureCommitRoute, "/api"), h.handleArchitectureCommit)

	// the architect channel: what passes between a task's architect and its
	// delegates, and the reviews it asks the person for
	g.GET("/task_messages", h.handleTaskMessages)
	g.GET("/review_requests", h.handleReviewRequests)
	g.POST("/respond_review_request", h.handleRespondReviewRequest)
	g.POST("/set_status_check", h.handleSetStatusCheck)

	// tickets & documents
	g.GET("/list_tickets", h.handleListTickets)
	g.GET("/get_ticket", h.handleGetTicket)
	g.POST("/create_ticket", h.handleCreateTicket)
	g.POST("/update_ticket", h.handleUpdateTicket)
	g.POST("/delete_ticket", h.handleDeleteTicket)
	g.POST("/archive_ticket", h.handleArchiveTicket)
	g.POST("/restore_ticket", h.handleRestoreTicket)
	g.POST("/archive_done_tickets", h.handleArchiveDoneTickets)
	g.GET("/list_documents", h.handleListDocuments)
	g.GET("/get_document", h.handleGetDocument)
	g.POST("/create_document", h.handleCreateDocument)
	g.POST("/update_document", h.handleUpdateDocument)
	g.POST("/set_document_scope", h.handleSetDocumentScope)
	g.POST("/delete_document", h.handleDeleteDocument)
	g.POST("/link_document_to_ticket", h.handleLinkDocument)
	g.POST("/unlink_document_from_ticket", h.handleUnlinkDocument)
	g.GET("/list_ticket_documents", h.handleListTicketDocuments)

	// task resources and comments: what a person adds to a task
	g.GET("/list_ticket_resources", h.handleListTicketResources)
	g.POST("/upload_ticket_resource", h.handleUploadTicketResource)
	g.POST("/add_ticket_link", h.handleAddTicketLink)
	g.POST("/update_ticket_resource", h.handleUpdateTicketResource)
	g.POST("/delete_ticket_resource", h.handleDeleteTicketResource)
	g.Match([]string{http.MethodGet, http.MethodHead}, "/ticket_resources/:id/content", h.handleTicketResourceContent)
	g.GET("/list_ticket_comments", h.handleListTicketComments)
	g.POST("/add_ticket_comment", h.handleAddTicketComment)
	g.POST("/update_ticket_comment", h.handleUpdateTicketComment)
	g.POST("/delete_ticket_comment", h.handleDeleteTicketComment)

	// notifications: the person's inbox across every project
	g.GET("/notifications", h.handleListNotifications)
	g.POST("/notifications/read", h.handleMarkNotificationsRead)
	g.GET("/notifications/events", h.handleNotificationEvents) // every inbox change (SSE)

	// usage: the token usage the LLM proxy measured, per project
	// (_harness is the harness's own spend)
	g.GET("/projects/:project/usage", h.handleUsageSummary) // ?range=today|7d|30d
	g.GET("/projects/:project/usage/records", h.handleListUsageRecords)
	g.GET("/projects/:project/usage/events", h.handleUsageEvents) // each record as it is stored (SSE)
	g.GET("/projects/:project/usage/live", h.handleUsageLive)     // the project's LLM calls as they happen (SSE)
	g.GET("/usage/totals", h.handleUsageTotals)                   // ?task_id= or ?session_id=

	// git credentials: what every git remote operation authenticates with
	g.GET("/list_git_credentials", h.handleListGitCredentials)
	g.POST("/create_git_credential", h.handleCreateGitCredential)
	g.POST("/update_git_credential", h.handleUpdateGitCredential)
	g.POST("/delete_git_credential", h.handleDeleteGitCredential)
	g.POST("/test_git_credential", h.handleTestGitCredential)
	g.GET("/get_git_identity", h.handleGetGitIdentity)
	g.POST("/update_git_identity", h.handleUpdateGitIdentity)
	g.GET("/get_repository_credential", h.handleGetRepositoryCredential)
	g.GET("/list_repository_credentials", h.handleListRepositoryCredentials)
	g.POST("/set_repository_credential", h.handleSetRepositoryCredential)

	// repository architecture: one structured record per repository
	g.POST("/get_repository_architecture", h.handleGetRepositoryArchitecture)
	g.POST("/save_repository_architecture", h.handleSaveRepositoryArchitecture)
	g.POST("/delete_repository_architecture", h.handleDeleteRepositoryArchitecture)
	g.POST("/preview_layers", h.handlePreviewLayers)
	g.POST("/export_repository_architecture", h.handleExportRepositoryArchitecture)
	g.POST("/list_architecture_rule_events", h.handleListArchitectureRuleEvents)
	g.POST("/discover_repository_architecture", h.handleDiscoverRepositoryArchitecture)
	g.GET("/architecture.schema.json", h.handleArchitectureSchema)

	// code views: read-only file trees, contents, diffs and the zone map
	g.GET("/code/tree", h.handleCodeTree)
	g.GET("/code/paths", h.handleCodePaths)
	g.GET("/code/file", h.handleCodeFile)
	g.GET("/code/diff", h.handleCodeDiff)
	g.GET("/code/map", h.handleCodeMap)

	// token savers: downloaded from the releases the harness pins
	g.GET("/list_token_savers", h.handleListTokenSavers)
	g.POST("/install_token_saver", h.handleInstallTokenSaver)
	g.POST("/remove_token_saver", h.handleRemoveTokenSaver)
	// repository indexes: codegraph's, built in each repository's checkout
	g.GET("/list_repository_indexes", h.handleListRepositoryIndexes)
	g.POST("/rebuild_repository_index", h.handleRebuildRepositoryIndex)

	return e
}

// Package web is the server half of the web client: a Backend For Frontend
// that renders pages with templ from the driving ports, in process. The
// browser half (web/) takes over once a page has loaded; see docs/WEB.md.
package web

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/agentisolation"
	"operators-mcp/internal/adapter/in/web/agents"
	"operators-mcp/internal/adapter/in/web/architecture"
	"operators-mcp/internal/adapter/in/web/code"
	"operators-mcp/internal/adapter/in/web/credentials"
	"operators-mcp/internal/adapter/in/web/designsystem"
	"operators-mcp/internal/adapter/in/web/history"
	"operators-mcp/internal/adapter/in/web/home"
	"operators-mcp/internal/adapter/in/web/models"
	"operators-mcp/internal/adapter/in/web/notifications"
	"operators-mcp/internal/adapter/in/web/projects"
	"operators-mcp/internal/adapter/in/web/serversettings"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/adapter/in/web/tasks"
	"operators-mcp/internal/adapter/in/web/tokensavers"
	"operators-mcp/internal/adapter/in/web/usage"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/pagetime"
	"operators-mcp/internal/infra/readshare"
	"operators-mcp/internal/ports"
)

// Deps are the driving ports the pages read. Each page takes the narrowest.
type Deps struct {
	Projects ports.ProjectReader
	Tasks    ports.TicketReader
	Sessions ports.SessionReader
	// Agents and Repositories fill the new-session form.
	Agents       ports.AgentNameLister
	Repositories ports.RepositoryLister
	// EnvFiles lists a repository's env files for their page.
	EnvFiles ports.EnvFileLister
	// RunCommands lists a repository's run commands for its settings page;
	// nil leaves the page's Run commands section out.
	RunCommands ports.RunCommandLister
	// Documents reads a task's documents and the project's library; nil hides both.
	Documents ports.DocumentReader
	// History reads repositories' commit graphs for their history pages.
	History ports.RepositoryHistory
	// Artifacts lists the design assets the task pages and the project's
	// design library seed; nil seeds none.
	Artifacts ports.ArtifactReader
	// Resources and Comments read what a person added to a task, for the
	// task page's first paint (the Web UI spec renders them); nil seeds none.
	Resources ports.TaskResourceReader
	Comments  ports.TaskCommentReader
	// ProjectSummaries counts every project for the projects list; nil
	// leaves /projects unserved and the top bar offers New project instead.
	ProjectSummaries ports.ProjectSummaries
	// AgentBoard serves the home page, the Agent board: every task with a
	// live session. Nil keeps the old home page, which opens the first project.
	AgentBoard ports.AgentBoard
	// PullRequests offers a session's branch as a pull request on its history
	// page and badges the task page's rows; nil leaves both out.
	PullRequests ports.PullRequests
	// WorktreeGit seeds the task page's session rows (and the open session)
	// with their worktrees' git state; nil seeds none.
	WorktreeGit ports.WorktreeGit
	// Code reads code for the code pages, the session's Code tab and the
	// history pages' diffs; nil leaves all three out.
	Code ports.CodeBrowser
	// Workspaces names a session's base branch and lists a repository's
	// branches for the code pages' ref select; nil shortens both.
	Workspaces CodeWorkspaces
	// Notifications seeds the top bar's unread count and the inbox's latest
	// page; nil, or a failed read, renders the page without a seed.
	Notifications ports.NotificationReader
	// GitCredentials seeds the Git credentials page, the repositories'
	// credential and the pull-request panel; nil leaves them out.
	GitCredentials ports.GitCredentials
	// TokenSaverInstalls lists the token savers for their page; nil leaves
	// the page and its top bar link out.
	TokenSaverInstalls ports.TokenSaverInstallReader
	// AgentCatalog reads the agent catalog for the Agents page's first paint;
	// nil, or a failed read, renders the page without a seed and the browser
	// reads the catalog itself. The page and its top bar link are always served.
	AgentCatalog CatalogReader
	// Settings reads each session tier's model for the Models page; nil
	// leaves the page and its top bar link out.
	Settings ports.SettingsReader
	// SandboxSettings and SandboxDriver fill the Agent isolation page: the
	// person's sandbox switch and extra paths, and the driver the platform
	// has (a nil func or result: none). A nil SandboxSettings leaves the page
	// and its top bar link out.
	SandboxSettings ports.SandboxSettings
	SandboxDriver   func() ports.SandboxDriver
	// ManagedClaude and ManagedClaudeBin fill the page's Managed claude
	// section: the installed versions, and the binary agents resolve to (the
	// override included). Nil leaves the section showing none installed.
	ManagedClaude    ports.ManagedClaude
	ManagedClaudeBin ports.ManagedClaudeBin
	// SandboxBuiltin lists the allowlist every session gets, for the page's
	// read-only list; nil lists none.
	SandboxBuiltin func() []ports.SandboxBuiltinGroup
	// AgentCredential fills the Agent isolation page's credential section
	// (status only, never the value); nil shows it as not set.
	AgentCredential ports.AgentCredential
	// AgentOAuth is the credential section's Sign in with Claude flow; nil
	// leaves the button disabled.
	AgentOAuth ports.AgentOAuth
	// AgentAuth is `claude auth status` of the agent home, read when the page
	// loads; nil shows no account.
	AgentAuth ports.AgentAuth
	// AgentHomeExposed says the agent Claude home lies in a git work tree
	// that does not ignore it; the credential section then warns. Nil: no
	// check.
	AgentHomeExposed func() bool
	// AgentLogin says the agent home holds a Claude login, which agents use
	// before the pasted credential; the credential section says which is in
	// use. Nil: no login.
	AgentLogin func() bool
	// DataPaths are where the server keeps its data, for the Server page;
	// nil leaves the page and its top bar link out.
	DataPaths *serversettings.Paths
	// DesignSystems resolves a design system page's slug and reads the record
	// for its first paint; nil leaves the pages not found.
	DesignSystems ports.DesignSystemPages
	// Interviews seeds each session's interviews on the task page; nil
	// seeds none.
	Interviews ports.InterviewReader
	// Usage reads token usage for a project's Usage page and the task
	// page's totals; nil leaves the page, its rail link and the totals out.
	Usage ports.UsageDashboard
	// TokenSavers reads token saver activations for the Token savers control
	// of the project's Settings page, the repository's settings page and
	// the task page; nil leaves the control out.
	TokenSavers ports.TokenSaverActivationReader
	// RepositoryIndexes lists the repository indexes on the Token savers
	// page; nil leaves that section out.
	RepositoryIndexes ports.RepositoryIndexes
	// ArchivedTasks counts the project's archived tasks for the project
	// page and the shell (the "Archived (N)" link); nil counts none.
	ArchivedTasks ports.ArchivedTaskCounter
	// ArchivedList lists the project's archived tasks for the Archived view;
	// nil lists none.
	ArchivedList tasks.ArchivedLister
	// WritingHelper reads whether the writing helper can run, for the pens of
	// every page and the Models page's row. It must not start anything; nil
	// reads as unavailable.
	WritingHelper func(ctx context.Context) shell.Helper
	// ArchitectureAssistant reads whether the architecture assistant can run,
	// for the Architecture page's seed and the Models page's row. It must not
	// start anything; nil reads as unavailable.
	ArchitectureAssistant ports.ArchitectureAssistantReadiness
	// Now defaults to time.Now.
	Now func() time.Time
}

// CatalogReader reads the whole agent catalog as GET /api/get_agent_catalog
// answers it, for the Agents page's first paint.
type CatalogReader interface {
	GetAgentCatalog(ctx context.Context) (json.RawMessage, error)
}

// CodeWorkspaces is what the code pages read of the workspaces:
// *workspaces.Service satisfies it.
type CodeWorkspaces interface {
	GetMany(ctx context.Context, ids []string) []*domain.Workspace
	ListBranches(repositoryID string) ([]domain.GitBranch, error)
}

// NewHandler routes the web client's pages and assets. Requests it does not
// own go to fallback (nil: 404), which keeps the legacy designer reachable
// while the two coexist.
func NewHandler(deps Deps, assets *Assets, fallback http.Handler) http.Handler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if fallback == nil {
		fallback = http.NotFoundHandler()
	}
	// Every page and the layout around it share one read of the project's
	// tickets and sessions per request (see pageLists).
	if deps.Tasks != nil {
		deps.Tasks = sharedTickets{deps.Tasks}
	}
	if deps.Sessions != nil {
		deps.Sessions = sharedSessions{deps.Sessions}
	}
	s := &server{deps: deps, assets: assets}

	homePage := home.Handler{Projects: deps.Projects, Board: deps.AgentBoard, Layout: s.layout, Render: render, Now: deps.Now}
	taskPages := tasks.Handler{
		Projects: deps.Projects, Tasks: deps.Tasks, Sessions: deps.Sessions,
		Agents: deps.Agents, Repositories: deps.Repositories, Docs: deps.Documents, Artifacts: deps.Artifacts, PullRequests: deps.PullRequests, WorktreeGit: deps.WorktreeGit,
		Resources: deps.Resources, Comments: deps.Comments, Code: deps.Code, Usage: deps.Usage, TokenSavers: deps.TokenSavers, Interviews: deps.Interviews,
		ArchivedTasks: deps.ArchivedList, Layout: s.layout, Render: render, Now: deps.Now,
	}
	if deps.Workspaces != nil {
		taskPages.Workspaces = deps.Workspaces
	}
	projectPages := projects.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, EnvFiles: deps.EnvFiles, RunCommands: deps.RunCommands, Summaries: deps.ProjectSummaries, Credentials: deps.GitCredentials,
		TokenSavers: deps.TokenSavers, Layout: s.layout, Render: render, Now: deps.Now,
	}
	historyPages := history.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, History: deps.History, Sessions: deps.Sessions, Tasks: deps.Tasks,
		PullRequests: deps.PullRequests, Code: deps.Code, WorktreeGit: deps.WorktreeGit, Layout: s.layout, Render: render, Now: deps.Now,
	}
	codePages := code.Handler{
		Projects: deps.Projects, Repositories: deps.Repositories, Code: deps.Code, History: deps.History,
		Layout: s.layout, Render: render,
	}
	if deps.Workspaces != nil {
		codePages.Branches = deps.Workspaces
	}
	architecturePages := architecture.Handler{Projects: deps.Projects, Repositories: deps.Repositories, Assistant: deps.ArchitectureAssistant, Layout: s.layout, Render: render}
	designSystemPage := designsystem.Handler{Projects: deps.Projects, Designs: deps.DesignSystems, Agents: deps.Agents, Layout: s.layout, Render: render}
	toProject := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/projects/"+url.PathEscape(r.PathValue("project")), http.StatusFound)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /static/", assets.Handler())
	mux.Handle("GET /{$}", s.page(homePage.Index))
	mux.HandleFunc("GET /switch-project", switchProject)
	if deps.Notifications != nil {
		inbox := notifications.Handler{Notifications: deps.Notifications, Projects: deps.Projects, Layout: s.layout, Render: render, Now: deps.Now}
		mux.Handle("GET "+notifications.Href, s.page(inbox.Page))
	}
	if deps.ProjectSummaries != nil {
		mux.Handle("GET /projects", s.page(projectPages.List))
	}
	if deps.GitCredentials != nil {
		creds := credentials.Handler{Credentials: deps.GitCredentials, Layout: s.layout, Render: render, Now: deps.Now}
		mux.Handle("GET "+credentials.Href, s.page(creds.Page))
	}
	if deps.TokenSaverInstalls != nil {
		savers := tokensavers.Handler{Savers: deps.TokenSaverInstalls, Layout: s.layout, Render: render}
		savers.Indexes, savers.Now = deps.RepositoryIndexes, deps.Now
		mux.Handle("GET "+tokensavers.Href, s.page(savers.Page))
	}
	agentPages := agents.Handler{Layout: s.layout, Render: render}
	if deps.AgentCatalog != nil {
		agentPages.Catalog = deps.AgentCatalog
	}
	mux.Handle("GET "+agents.Href, s.page(agentPages.Page))
	mux.Handle("GET "+agents.Href+"/{path...}", s.page(agentPages.Page))
	if deps.Settings != nil {
		modelsPage := models.Handler{Settings: deps.Settings, Helper: s.helper, Architecture: s.architectureAssistant, Layout: s.layout, Render: render}
		mux.Handle("GET "+models.Href, s.page(modelsPage.Page))
	}
	if deps.SandboxSettings != nil {
		isolation := agentisolation.Handler{Sandbox: deps.SandboxSettings, Driver: deps.SandboxDriver, Credential: deps.AgentCredential, OAuth: deps.AgentOAuth, Auth: deps.AgentAuth, Layout: s.layout, Render: render,
			Settings: deps.Settings, Claude: deps.ManagedClaude, ClaudeBin: deps.ManagedClaudeBin, Builtin: deps.SandboxBuiltin, HomeExposed: deps.AgentHomeExposed, HasLogin: deps.AgentLogin, Projects: deps.Projects}
		mux.Handle("GET "+agentisolation.Href, s.page(isolation.Page))
	}
	if deps.DataPaths != nil {
		serverPage := serversettings.Handler{Paths: *deps.DataPaths, Settings: deps.Settings, Layout: s.layout, Render: render}
		mux.Handle("GET "+serversettings.Href, s.page(serverPage.Page))
	}
	mux.Handle("GET /projects/new", s.page(projectPages.New))
	mux.Handle("GET /projects/{project}", s.page(taskPages.Project))
	mux.Handle("GET /projects/{project}/settings", s.page(projectPages.Settings))
	mux.Handle("GET /projects/{project}/repositories", s.page(projectPages.RepositoryList))
	mux.Handle("GET /projects/{project}/repositories/{repository}/env", s.page(projectPages.EnvFileList))
	for tab, slug := range map[string]string{"": "", "/instructions": "instructions", "/tools": "tools", "/sandboxes": "sandboxes", "/orchestration": "orchestration", "/rules": "rules", "/layers": "layers", "/file": "file", "/diagram": "diagram", "/agents": "agents"} {
		mux.Handle("GET /projects/{project}/repositories/{repository}/architecture"+tab, s.page(architecturePages.Page(slug)))
	}
	mux.Handle("GET /projects/{project}/repositories/{repository}/history", s.page(historyPages.Page))
	mux.Handle("GET /projects/{project}/repositories/{repository}/history/{commit}", s.page(historyPages.Page))
	if deps.Code != nil {
		mux.Handle("GET /projects/{project}/code", s.page(codePages.Index))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code", s.page(codePages.Root))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/files", s.page(codePages.Files))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/files/{path...}", s.page(codePages.Files))
		mux.Handle("GET /projects/{project}/repositories/{repository}/code/map", s.page(codePages.Map))
		session := "GET /projects/{project}/tasks/{task}/sessions/{session}/code"
		mux.Handle(session, s.page(taskPages.SessionCode("")))
		mux.Handle(session+"/changes", s.page(taskPages.SessionCode(code.ViewChanges)))
		mux.Handle(session+"/files", s.page(taskPages.SessionCode(code.ViewFiles)))
		mux.Handle(session+"/files/{path...}", s.page(taskPages.SessionCode(code.ViewFiles)))
		mux.Handle(session+"/map", s.page(taskPages.SessionCode(code.ViewMap)))
	}
	mux.Handle("GET /projects/{project}/design", s.page(taskPages.ProjectDesign))
	mux.Handle("GET /projects/{project}/design-systems", s.page(designSystemPage.Index))
	mux.Handle("GET /projects/{project}/design-systems/{slug}", s.page(designSystemPage.Page))
	mux.Handle("GET /projects/{project}/reviews", s.page(taskPages.Reviews))
	mux.Handle("GET /projects/{project}/archived", s.page(taskPages.Archived))
	if deps.Usage != nil {
		usagePage := usage.Handler{Projects: deps.Projects, Usage: deps.Usage, Layout: s.layout, Render: render}
		mux.Handle("GET /projects/{project}/usage", s.page(usagePage.Page))
	}
	mux.Handle("GET /projects/{project}/documents", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}", s.page(taskPages.ProjectDocuments))
	mux.Handle("GET /projects/{project}/documents/{document}/view", s.page(taskPages.ProjectDocumentView))
	mux.HandleFunc("GET /projects/{project}/{$}", toProject)
	mux.Handle("GET /projects/{project}/sessions", s.page(taskPages.ProjectSessions)) // the project's feature brainstorms: sessions with no task
	mux.Handle("GET /projects/{project}/tasks/new", s.page(taskPages.New))
	mux.Handle("GET /projects/{project}/tasks/{task}", s.page(taskPages.Task))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/sessions/{session}/history/{commit}", s.page(historyPages.Session))
	mux.Handle("GET /projects/{project}/tasks/{task}/design", s.page(taskPages.TaskDesign))
	mux.Handle("GET /projects/{project}/tasks/{task}/resources", s.page(taskPages.TaskResources))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents", s.page(taskPages.Documents))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents/{document}", s.page(taskPages.Documents))
	mux.Handle("GET /projects/{project}/tasks/{task}/documents/{document}/view", s.page(taskPages.DocumentView))
	mux.Handle("/", fallback)
	return mux
}

type server struct {
	deps   Deps
	assets *Assets
}

// page adapts a page handler: an error becomes the error page for its code.
func (s *server) page(h shell.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := shell.WithCollapsed(r.Context(), r) // the rail's folded groups, for the layout
		shell.VaryOnPage(w.Header())
		if shell.PageRequest(r) {
			if shell.SameFrame(r) {
				ctx = shell.WithMode(ctx, shell.ModeFragment) // <main> and what the frame shows for it, without the frame
			} else {
				ctx = shell.WithMode(ctx, shell.ModeShell) // the new frame, without a new document
			}
		}
		// One read of what the page, its layout and its parts read alike:
		// the project's ticket and session lists, a worktree's status.
		ctx = readshare.With(ctx)
		var timing *pagetime.Timing
		if r.PathValue("project") != "" {
			ctx, timing = pagetime.Start(ctx)
			defer timing.Bind()()
		}
		r = r.WithContext(ctx)
		if err := h(w, r); err != nil {
			s.renderError(w, r, err)
		}
		if timing != nil {
			logPageTime(r, timing)
		}
	})
}

// switchProject is the project picker's target: a GET form, so switching
// works without JavaScript too.
func switchProject(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("project")
	if id == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(id), http.StatusFound)
}

// layout builds the shared frame: the assets, the project picker, and the
// selected project's tasks on the rail with their live sessions.
func (s *server) layout(ctx context.Context, title, projectID, taskID string) (shell.Frame, error) {
	if shell.IsFragment(ctx) {
		return s.fragmentFrame(ctx, title, projectID, taskID)
	}
	started := time.Now()
	defer func() { pagetime.AddFrame(ctx, time.Since(started)) }()
	frame := s.bareFrame(title)
	frame.Notifications = s.bell(ctx)
	frame.Helper = s.helper(ctx)
	all, err := s.deps.Projects.ListProjects(ctx)
	if err != nil {
		return shell.Frame{}, err
	}
	slices.SortFunc(all, func(a, b *domain.Project) int {
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	frame.Top.Current = projectID
	if s.deps.ProjectSummaries != nil {
		frame.Top.ProjectsHref = projects.ListHref
	}
	for _, p := range all {
		frame.Top.Projects = append(frame.Top.Projects, shell.Option{ID: p.ID, Name: p.Name})
	}
	if projectID == "" {
		return frame, nil
	}

	list, err := s.deps.Tasks.ListTickets(ctx, projectID, ports.TicketFilter{})
	if err != nil {
		return shell.Frame{}, err
	}
	live, err := s.deps.Sessions.List(ctx, ports.SessionFilter{ProjectID: projectID})
	if err != nil {
		return shell.Frame{}, err
	}
	frame.Rail = tasks.BuildRail(projectID, list, live, taskID, shell.CollapsedFrom(ctx))
	if s.deps.ArchivedTasks != nil {
		if frame.Rail.Seed.ArchivedCount, err = s.deps.ArchivedTasks.ArchivedTaskCount(ctx, projectID); err != nil {
			return shell.Frame{}, err
		}
	}
	frame.Rail.BoardHref = tasks.BoardHref(projectID)
	if s.deps.Code != nil {
		frame.Rail.CodeHref = code.IndexHref(projectID)
	}
	frame.Rail.NewTaskHref = tasks.NewTaskHref(projectID)
	frame.Rail.ReviewsHref = tasks.ReviewsHref(projectID)
	if s.deps.Documents != nil {
		frame.Rail.DocumentsHref = tasks.ProjectDocumentsHref(projectID, "")
	}
	frame.Rail.DesignHref = tasks.ProjectDesignHref(projectID)
	frame.Rail.DesignSystemHref = designsystem.IndexHref(projectID)
	frame.Rail.DesignSystemProposals = s.openDesignProposals(ctx, projectID)
	if s.deps.Usage != nil {
		frame.Rail.UsageHref = usage.Href(projectID)
	}
	return frame, nil
}

// fragmentFrame is what a page fragment needs of the frame: the page's title
// and what a handler reads back from it. The browser already holds the top
// bar, the rail and the bell, so none of them is read (ADR 0064).
func (s *server) fragmentFrame(ctx context.Context, title, projectID, taskID string) (shell.Frame, error) {
	frame := shell.Frame{Title: title, Built: true}
	frame.Helper = s.helper(ctx)
	frame.Rail.Current = taskID
	// The top bar stays in the document; which of its pages is open is named
	// by its href, so the browser can mark it.
	s.topLinks(&frame.Top)
	if s.deps.ProjectSummaries != nil {
		frame.Top.ProjectsHref = projects.ListHref
	}
	if s.deps.ArchivedTasks != nil && projectID != "" {
		// The board paints the archived count from the rail's seed.
		n, err := s.deps.ArchivedTasks.ArchivedTaskCount(ctx, projectID)
		if err != nil {
			return shell.Frame{}, err
		}
		frame.Rail.Seed = &shell.RailSeed{ProjectID: projectID, ArchivedCount: n}
	}
	return frame, nil
}

// openDesignProposals counts the proposals waiting on the person across the
// project's design systems; a failed read counts none, the badge follows the
// feed once the page is open.
func (s *server) openDesignProposals(ctx context.Context, projectID string) int {
	if s.deps.DesignSystems == nil {
		return 0
	}
	list, err := s.deps.DesignSystems.ListDesignSystems(ctx, projectID)
	if err != nil {
		return 0
	}
	n := 0
	for _, d := range list {
		n += d.OpenProposals
	}
	return n
}

// bell is the top bar's way to the inbox, with the unread count and the
// panel's latest notifications; nil when the inbox is not served. A failed
// read still shows the bell: the browser reads the count itself.
func (s *server) bell(ctx context.Context) *shell.Bell {
	if s.deps.Notifications == nil {
		return nil
	}
	b := &shell.Bell{Href: notifications.Href}
	page, err := s.deps.Notifications.ListNotifications(ctx, ports.NotificationQuery{Limit: notifications.PanelLimit})
	if err != nil {
		slog.Warn("web: notifications seed not read; the browser reads it", "err", err)
		return b
	}
	b.Seed = page
	return b
}

// helper is the writing helper's readiness: unavailable without a reader.
func (s *server) helper(ctx context.Context) shell.Helper {
	if s.deps.WritingHelper == nil {
		return shell.Helper{}
	}
	return s.deps.WritingHelper(ctx)
}

// architectureAssistant is the architecture assistant's readiness for the
// Models page: unavailable without a reader.
func (s *server) architectureAssistant(ctx context.Context) shell.Helper {
	if s.deps.ArchitectureAssistant == nil {
		return shell.Helper{Reason: "the architecture assistant is not configured on this server"}
	}
	a := s.deps.ArchitectureAssistant.Readiness(ctx)
	return shell.Helper{Available: a.Available, Reason: a.Reason, FixURL: a.FixURL}
}

// topLinks names the top bar's pages that this server serves.
func (s *server) topLinks(top *shell.TopBar) {
	if s.deps.AgentBoard != nil {
		top.BoardHref = home.BoardHref
	}
	top.AgentsHref = agents.Href
	if s.deps.GitCredentials != nil {
		top.CredentialsHref = credentials.Href
	}
	if s.deps.TokenSaverInstalls != nil {
		top.TokenSaversHref = tokensavers.Href
	}
	if s.deps.Settings != nil {
		top.ModelsHref = models.Href
	}
	if s.deps.SandboxSettings != nil {
		top.AgentIsolationHref = agentisolation.Href
	}
	if s.deps.DataPaths != nil {
		top.ServerHref = serversettings.Href
	}
}

// bareFrame is a frame without the rail, for when the rail cannot be read.
func (s *server) bareFrame(title string) shell.Frame {
	f := shell.Frame{
		Title: title,
		CSS:   s.assets.URL("app.css"),
		JS:    s.assets.URL("app.js"),
		Built: s.assets.Built(),
	}
	f.Helper = s.helper(context.Background())
	s.topLinks(&f.Top)
	if s.deps.SandboxSettings != nil {
		f.Top.AgentIsolationAttention = agentisolation.Blocks(s.deps.SandboxSettings, s.deps.SandboxDriver, s.deps.AgentCredential, s.deps.AgentLogin, s.deps.ManagedClaudeBin)
	}
	return f
}

// renderError shows a coded error the way the API reports it: the message,
// the code, and what to do next. A *_NOT_FOUND is a 404; anything uncoded is
// a 500 whose details stay in the log.
func (s *server) renderError(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		// The request ended: nobody reads the page, and the reads it cut
		// short are not a server fault.
		return
	}
	code := errs.Code(err)
	status, message := http.StatusInternalServerError, "Something went wrong on the server."
	switch {
	case strings.HasSuffix(code, "_NOT_FOUND"):
		status, message = http.StatusNotFound, errorMessage(err)
	case code != "":
		message = errorMessage(err)
	default:
		code = "INTERNAL"
		slog.Error("web page failed", "path", r.URL.Path, "err", err)
	}
	if renderErr := render(w, r, status, errorPage(s.bareFrame("Error"), message, code, nextStep(code))); renderErr != nil {
		slog.Error("web error page failed", "path", r.URL.Path, "err", renderErr)
	}
}

func errorMessage(err error) string {
	var se *domain.StructuredError
	if errors.As(err, &se) && se.Message != "" {
		return se.Message
	}
	return err.Error()
}

func nextStep(code string) string {
	switch code {
	case "PROJECT_NOT_FOUND":
		return "pick another project at the top"
	case "TICKET_NOT_FOUND":
		return "it was deleted or moved; pick a task on the rail"
	case "SESSION_NOT_FOUND":
		return "it was deleted; go back to the list"
	case "INTERNAL":
		return "check the server log, then reload"
	default:
		return "reload, or check the server log"
	}
}

// render writes page with status. It renders to a buffer first, so a template
// failure becomes an error instead of half a page.
func render(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error {
	var buf bytes.Buffer
	rendering := time.Now()
	if err := page.Render(r.Context(), &buf); err != nil {
		return err
	}
	if t := pagetime.From(r.Context()); t != nil {
		w.Header().Set("Server-Timing", t.Report(time.Since(rendering)).Header())
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusOK && shell.Prefetched(r) {
		// Held for the click that follows the hover, and dropped after 5 s.
		w.Header().Set("Cache-Control", shell.PrefetchHold)
	}
	shell.MarkAnswer(w.Header(), shell.ModeOf(r.Context()))
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// logPageTime writes the request's page server time and its breakdown, with
// the route and the project, as one log line. The header carries the time
// up to the write; this line carries it after.
func logPageTime(r *http.Request, t *pagetime.Timing) {
	rep := t.Report(0)
	slog.Info("page time", "route", r.Pattern, "project", r.PathValue("project"),
		"total", rep.Total, "db", rep.DB, "git", rep.Git, "git_calls", rep.GitCalls, "render", rep.Render, "frame", rep.Frame, "catalog", rep.Catalog, "catalog_reads", rep.CatalogReads)
}

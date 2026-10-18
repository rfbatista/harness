package tasks

import (
	"context"
	"log/slog"
	"net/http"
	"operators-mcp/internal/adapter/in/web/tokensavers"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/code"
	"operators-mcp/internal/adapter/in/web/pullrequests"
	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Handler serves a project (pick a task) and a task (its sessions).
type Handler struct {
	Projects     ports.ProjectReader
	Tasks        ports.TicketReader
	Sessions     ports.SessionReader
	Agents       ports.AgentLister
	Repositories ports.RepositoryLister
	// Docs reads the documents linked to a task and the project's library;
	// nil hides both.
	Docs ports.DocumentReader
	// Artifacts lists the design assets the task page and the design library
	// seed; nil seeds none.
	Artifacts ports.ArtifactReader
	// Resources and Comments read what a person added to a task, for its
	// resources page and the task toolbar's counts; nil reads none.
	Resources ports.TaskResourceReader
	Comments  ports.TaskCommentReader
	// PullRequests seeds the session rows' pull request badges; nil seeds none.
	PullRequests ports.PullRequests
	// Usage seeds the task's and its sessions' token totals; nil leaves them out.
	Usage ports.UsageDashboard
	// Code serves the sessions' Code tab; nil leaves it out. Workspaces
	// names each worktree's base branch for it.
	Code       ports.CodeBrowser
	Workspaces WorkspaceReader
	// TokenSavers seeds the task's Token savers control; nil leaves it out.
	TokenSavers ports.TokenSaverActivationReader
	Layout      shell.Layout
	Render      shell.Renderer
	Now         func() time.Time
}

// workspaceIDs are the sessions' worktrees, each once.
func workspaceIDs(list []*domain.Session) []string {
	seen := map[string]bool{}
	ids := []string{}
	for _, s := range list {
		if s.WorkspaceID != "" && !seen[s.WorkspaceID] {
			seen[s.WorkspaceID] = true
			ids = append(ids, s.WorkspaceID)
		}
	}
	return ids
}

// Project serves GET /projects/{project}: the project's tasks as a kanban
// board, live over the project feed, beside the rail.
func (h Handler) Project(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	list, err := h.Tasks.ListTickets(ctx, project.ID)
	if err != nil {
		return err
	}
	live, err := h.Sessions.List(ctx, ports.SessionFilter{ProjectID: project.ID})
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, project.Name, project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true             // the stream bar shows the connection…
	frame.Rail.ReportsFeed = true // …which the rail, the page's one feed, reports
	frame.Rail.Page = shell.PageBoard
	return h.Render(w, r, http.StatusOK, ProjectPage(frame, project.ID, project.Name, BuildBoard(project.ID, list, live)))
}

// New serves GET /projects/{project}/tasks/new: the new-task form.
func (h Handler) New(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, "New task", project.ID, "")
	if err != nil {
		return err
	}
	return h.Render(w, r, http.StatusOK, NewTaskPage(frame, project.ID, project.Name))
}

// Task serves GET /projects/{project}/tasks/{task}: the task's sessions. A
// task of another project is TICKET_NOT_FOUND, as if it did not exist.
func (h Handler) Task(w http.ResponseWriter, r *http.Request) error {
	return h.task(w, r, nil)
}

// WorkspaceReader reads a workspace (*workspaces.Service satisfies it).
type WorkspaceReader interface {
	Get(id string) *domain.Workspace
}

// SessionCode serves a session's code URLs (…/sessions/{session}/code[/…]):
// the task page with that session open on its Code tab, painted from the
// code browser in process.
func (h Handler) SessionCode(view string) shell.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		if h.Code == nil {
			return errs.Newf("UNAVAILABLE", "code browsing is not available on this server")
		}
		rt := code.SessionRoute(r, view)
		return h.task(w, r, &rt)
	}
}

func (h Handler) task(w http.ResponseWriter, r *http.Request, codeRoute *code.Route) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	task, err := h.Tasks.GetTicket(ctx, r.PathValue("task"))
	if err != nil {
		return err
	}
	if task.ProjectID != project.ID {
		return errs.Newf("TICKET_NOT_FOUND", "task %s is not in project %s", task.ID, project.Name)
	}
	list, err := h.Sessions.List(ctx, ports.SessionFilter{ProjectID: project.ID, TicketID: task.ID})
	if err != nil {
		return err
	}
	agents, err := h.Agents.ListAgents(ctx)
	if err != nil {
		return err
	}
	repos, err := h.Repositories.ListRepositories(ctx, project.ID)
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, task.Title, project.ID, task.ID)
	if err != nil {
		return err
	}
	frame.Live = true // sessionsPage follows the project's feed
	view := sessions.NewPageView(frame, project, task, list, agents, repos, h.Now())
	if view.Seed.Artifacts, err = h.designArtifacts(ctx, ports.ArtifactFilter{TicketID: task.ID}); err != nil {
		return err
	}
	tickets, err := h.Tasks.ListTickets(ctx, project.ID)
	if err != nil {
		return err
	}
	view.Seed.Tasks = designTasks(tickets, project.ID)
	var docs []*domain.Document
	if h.Docs != nil {
		docs = h.Docs.ListTicketDocuments(task.ID)
		view.Documents = &sessions.DocumentsLink{
			Href:      DocumentsHref(project.ID, task.ID, ""),
			TicketID:  task.ID,
			Count:     len(docs),
			Signature: documentSignature(docs),
		}
		view.Seed.DocumentTitles = make(map[string]string, len(docs))
		for _, d := range docs {
			view.Seed.DocumentTitles[d.ID] = d.Title
		}
	}
	// The toolbar's counts are not worth the page: a failed read hides the links.
	if h.Resources != nil || h.Comments != nil {
		if resources, comments, err := h.resourcesOf(ctx, task.ID); err == nil {
			view.Resources = resourcesLink(project.ID, task.ID, resources, comments)
		}
	}
	view.Reviews = reviewsSeed(project.ID, task.ID, list, agents, []*domain.Ticket{task}, docs)
	if h.PullRequests != nil {
		view.PullRequests = pullrequests.NewWatchSeed(ctx, h.PullRequests, project.ID, workspaceIDs(list))
	}
	if h.Usage != nil {
		view.Usage = h.usageTotals(ctx, project.ID, task.ID, list)
	}
	view.TokenSavers = tokensavers.Read(ctx, h.TokenSavers, domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: task.ID})
	for _, st := range Statuses {
		view.StatusChoices = append(view.StatusChoices, sessions.StatusChoice{Value: st.Value, Label: st.Label, Selected: st.Value == string(task.Status)})
	}
	if h.Code != nil {
		view.Seed.Code = true
		view.Seed.BaseRefs = h.baseRefs(list)
	}
	if codeRoute != nil {
		paint, err := h.codePaint(ctx, *codeRoute, list, repos, view.Seed.BaseRefs)
		if err != nil {
			return err
		}
		view.Code = paint
		view.Seed.OpenCode = codeRoute.SessionID
	}
	return h.Render(w, r, http.StatusOK, sessions.Page(view))
}

// usageTotals reads the task's and each of its sessions' all-time token
// totals; nil when one cannot be read: the totals are not worth the page.
func (h Handler) usageTotals(ctx context.Context, projectID, taskID string, list []*domain.Session) *sessions.UsageTotalsSeed {
	seed := &sessions.UsageTotalsSeed{ProjectID: projectID, TaskID: taskID, Sessions: make(map[string]*ports.UsageTotals, len(list))}
	var err error
	if seed.Task, err = h.Usage.UsageTotals(ctx, ports.UsageTotalsQuery{TaskID: taskID}); err != nil {
		slog.Warn("task page: usage totals not read", "task", taskID, "err", err)
		return nil
	}
	for _, s := range list {
		if seed.Sessions[s.ID], err = h.Usage.UsageTotals(ctx, ports.UsageTotalsQuery{SessionID: s.ID}); err != nil {
			slog.Warn("task page: usage totals not read", "session", s.ID, "err", err)
			return nil
		}
	}
	return seed
}

// baseRefs names each session worktree's base branch, by workspace.
func (h Handler) baseRefs(list []*domain.Session) map[string]string {
	out := map[string]string{}
	if h.Workspaces == nil {
		return out
	}
	for _, s := range list {
		if s.WorkspaceID == "" {
			continue
		}
		if ws := h.Workspaces.Get(s.WorkspaceID); ws != nil && ws.BaseRef != "" {
			out[s.WorkspaceID] = ws.BaseRef
		}
	}
	return out
}

// codePaint reads what the session's Code tab shows first.
func (h Handler) codePaint(ctx context.Context, rt code.Route, list []*domain.Session, repos []*domain.Repository, baseRefs map[string]string) (*code.View, error) {
	var session *domain.Session
	for _, s := range list {
		if s.ID == rt.SessionID {
			session = s
		}
	}
	if session == nil {
		return nil, errs.Newf("SESSION_NOT_FOUND", "session %s is not on this task", rt.SessionID)
	}
	if session.WorkspaceID == "" {
		return nil, errs.Newf("WORKSPACE_NOT_FOUND", "session %s has no worktree", rt.SessionID)
	}
	name := ""
	for _, repo := range repos {
		if repo.ID == session.RepositoryID {
			name = repo.Name
		}
	}
	seed := code.Load(ctx, h.Code, rt, code.Source{
		Query:          ports.CodeSourceQuery{WorkspaceID: session.WorkspaceID},
		RepositoryName: name,
		Branch:         session.Branch,
		BaseRef:        baseRefs[session.WorkspaceID],
		Live:           !session.Status.IsTerminal(),
	})
	v := code.Build(seed)
	return &v, nil
}

package tasks

import (
	"net/http"
	"time"

	"github.com/rfbatista/harnesskit/errs"

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
	Layout    shell.Layout
	Render    shell.Renderer
	Now       func() time.Time
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
	view.Reviews = reviewsSeed(project.ID, task.ID, list, agents, []*domain.Ticket{task}, docs)
	for _, st := range Statuses {
		view.StatusChoices = append(view.StatusChoices, sessions.StatusChoice{Value: st.Value, Label: st.Label, Selected: st.Value == string(task.Status)})
	}
	return h.Render(w, r, http.StatusOK, sessions.Page(view))
}

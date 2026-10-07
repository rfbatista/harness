package tasks

import (
	"net/http"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// DesignHref is a task's design assets page.
func DesignHref(projectID, taskID string) string {
	return Href(projectID, taskID) + "/design"
}

// TaskDesignView is the frame of a task's design assets. The browser renders
// the list (sessionsTaskDesign) from the seed and follows the project feed.
type TaskDesignView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	TaskID      string
	TaskTitle   string
	TaskHref    string
	LibraryHref string
	Seed        TaskDesignSeed
}

// TaskDesignSeed is the task-design-seed JSON: the task's design assets
// (produced and attached, newest first) and the project's tasks, so an
// attached asset can link the task that produced it. Never null.
type TaskDesignSeed struct {
	ProjectID string              `json:"project_id"`
	TicketID  string              `json:"ticket_id"`
	Tasks     []sessions.TaskLink `json:"tasks"`
	Artifacts []*domain.Artifact  `json:"artifacts"`
}

// TaskDesign serves GET /projects/{project}/tasks/{task}/design: what the
// task's sessions made and the project assets attached to it. A task of
// another project is TICKET_NOT_FOUND, as if it did not exist.
func (h Handler) TaskDesign(w http.ResponseWriter, r *http.Request) error {
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
	tickets, err := h.Tasks.ListTickets(ctx, project.ID)
	if err != nil {
		return err
	}
	assets, err := h.designArtifacts(ctx, ports.ArtifactFilter{TicketID: task.ID})
	if err != nil {
		return err
	}
	frame, err := h.Layout(ctx, "Design assets · "+task.Title, project.ID, task.ID)
	if err != nil {
		return err
	}
	frame.Live = true             // the stream bar shows the connection…
	frame.Rail.ReportsFeed = true // …which the rail, following the same feed, reports
	view := TaskDesignView{
		Frame: frame, ProjectID: project.ID, ProjectName: project.Name,
		TaskID: task.ID, TaskTitle: task.Title, TaskHref: Href(project.ID, task.ID), LibraryHref: ProjectDesignHref(project.ID),
		Seed: TaskDesignSeed{ProjectID: project.ID, TicketID: task.ID, Tasks: designTasks(tickets, project.ID), Artifacts: assets},
	}
	return h.Render(w, r, http.StatusOK, TaskDesignPage(view))
}

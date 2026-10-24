package tasks

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/in/web/components"
	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// ResourcesHref is a task's resources and comments page.
func ResourcesHref(projectID, taskID string) string {
	return Href(projectID, taskID) + "/resources"
}

// TaskResourcesView is the frame of a task's resources and comments. The
// browser renders both lists (tasksResources) from the seed and follows the
// project feed.
type TaskResourcesView struct {
	Frame       shell.Frame
	ProjectID   string
	ProjectName string
	TaskID      string
	TaskTitle   string
	// TaskDescription is read by the writing helper beside a comment.
	TaskDescription string
	TaskHref        string
	Seed            TaskResourcesSeed
}

// TaskResourcesSeed is the task-resources-seed JSON: the task's resources
// and comments in the API's shapes, oldest first, and the ids of the
// sessions listed on the task, so a session's comment links to it while it
// is there. Never null.
type TaskResourcesSeed struct {
	ProjectID  string                   `json:"project_id"`
	TicketID   string                   `json:"ticket_id"`
	Resources  []*domain.TicketResource `json:"resources"`
	Comments   []*domain.TicketComment  `json:"comments"`
	SessionIDs []string                 `json:"session_ids"`
}

// TaskResources serves GET /projects/{project}/tasks/{task}/resources: what
// the person added to the task for context, and their comments. A task of
// another project is TICKET_NOT_FOUND, as if it did not exist.
func (h Handler) TaskResources(w http.ResponseWriter, r *http.Request) error {
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
	resources, comments, err := h.resourcesOf(ctx, task.ID)
	if err != nil {
		return err
	}
	list, err := h.Sessions.List(ctx, ports.SessionFilter{ProjectID: project.ID, TicketID: task.ID})
	if err != nil {
		return err
	}
	sessionIDs := make([]string, 0, len(list))
	for _, s := range list {
		sessionIDs = append(sessionIDs, s.ID)
	}
	frame, err := h.Layout(ctx, "Resources and comments · "+task.Title, project.ID, task.ID)
	if err != nil {
		return err
	}
	frame.Live = true             // the stream bar shows the connection…
	frame.Rail.ReportsFeed = true // …which the rail, following the same feed, reports
	frame.Hints = []shell.Hint{{Keys: []string{"⌘V"}, Label: "paste an image"}}
	view := TaskResourcesView{
		Frame: frame, ProjectID: project.ID, ProjectName: project.Name,
		TaskID: task.ID, TaskTitle: task.Title, TaskDescription: task.Description, TaskHref: Href(project.ID, task.ID),
		Seed: TaskResourcesSeed{ProjectID: project.ID, TicketID: task.ID, Resources: resources, Comments: comments, SessionIDs: sessionIDs},
	}
	return h.Render(w, r, http.StatusOK, TaskResourcesPage(view))
}

// resourcesOf reads a task's resources and comments; a nil reader reads none.
func (h Handler) resourcesOf(ctx context.Context, taskID string) ([]*domain.TicketResource, []*domain.TicketComment, error) {
	resources := []*domain.TicketResource{}
	comments := []*domain.TicketComment{}
	if h.Resources != nil {
		list, err := h.Resources.ListTicketResources(ctx, taskID)
		if err != nil {
			return nil, nil, err
		}
		resources = append(resources, list...)
	}
	if h.Comments != nil {
		list, err := h.Comments.ListTicketComments(ctx, taskID, time.Time{})
		if err != nil {
			return nil, nil, err
		}
		comments = append(comments, list...)
	}
	return resources, comments, nil
}

// resourcesLink is the task toolbar's way to the resources page: the ids of
// what is there, so the browser (tasksResourceWatch) counts the feed's
// changes once each.
func resourcesLink(projectID, taskID string, resources []*domain.TicketResource, comments []*domain.TicketComment) *sessions.ResourcesLink {
	rids := make([]string, 0, len(resources))
	for _, r := range resources {
		rids = append(rids, r.ID)
	}
	cids := make([]string, 0, len(comments))
	for _, c := range comments {
		cids = append(cids, c.ID)
	}
	return &sessions.ResourcesLink{
		Href:        ResourcesHref(projectID, taskID),
		ProjectID:   projectID,
		TicketID:    taskID,
		ResourceIDs: strings.Join(rids, ","),
		CommentIDs:  strings.Join(cids, ","),
		Resources:   len(resources),
		Comments:    len(comments),
	}
}

// commentPen is the magic pen on a comment field: it reads the task's title
// and description beside the comment, and its panel opens above the field,
// which sits at the bottom of its pane.
func commentPen(id string, v TaskResourcesView) components.Pen {
	return components.Pen{
		ID: id, Kind: "comment", Noun: "comment", Label: "Comment",
		ProjectID: v.ProjectID, ProjectName: v.ProjectName,
		Siblings: []components.PenSibling{
			{Label: "Task title", Text: v.TaskTitle},
			{Label: "Task description", Text: v.TaskDescription},
		},
		Available: v.Frame.Helper.Available, Reason: v.Frame.Helper.Reason, FixURL: v.Frame.Helper.FixURL,
		Above: id == "pen-comment",
	}
}

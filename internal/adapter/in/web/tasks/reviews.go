package tasks

import (
	"net/http"
	"strconv"

	"operators-mcp/internal/adapter/in/web/components"
	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Reviews serves GET /projects/{project}/reviews: the project's review inbox.
func (h Handler) Reviews(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	project, err := h.Projects.GetProject(ctx, r.PathValue("project"))
	if err != nil {
		return err
	}
	tickets, err := h.Tasks.ListTickets(ctx, project.ID)
	if err != nil {
		return err
	}
	list, err := h.Sessions.List(ctx, ports.SessionFilter{ProjectID: project.ID})
	if err != nil {
		return err
	}
	agents, err := h.Agents.ListAgents(ctx)
	if err != nil {
		return err
	}
	var docs []*domain.Document
	pending := 0
	for _, t := range tickets {
		pending += t.PendingReviews
		if h.Docs != nil && t.PendingReviews > 0 {
			docs = append(docs, h.Docs.ListTicketDocuments(t.ID)...)
		}
	}
	frame, err := h.Layout(ctx, "Reviews", project.ID, "")
	if err != nil {
		return err
	}
	frame.Live = true             // the stream bar shows the connection…
	frame.Rail.ReportsFeed = true // …which the rail, following the same feed, reports
	seed := reviewsSeed(project.ID, "", list, agents, tickets, docs)
	return h.Render(w, r, http.StatusOK, ReviewsPage(frame, project.ID, project.Name, seed, pending))
}

// reviewsWaiting is the inbox's count: "1 review waits on you". The
// browser's waitingLine (reviews/presentation/view.js) says the same.
func reviewsWaiting(n int) string {
	if n == 1 {
		return "1 review waits on you"
	}
	return strconv.Itoa(n) + " reviews wait on you"
}

// reviewsSeed names what the review band and the inbox show: sessions as
// "go-developer · Server: architect channel", tasks with their page, and
// documents by title. ticketID is "" on the project's inbox.
func reviewsSeed(projectID, ticketID string, list []*domain.Session, agents []*domain.Agent, tickets []*domain.Ticket, docs []*domain.Document) *components.ReviewsSeed {
	names := make(map[string]string, len(agents))
	for _, a := range agents {
		names[a.ID] = a.Name
	}
	seed := &components.ReviewsSeed{
		ProjectID: projectID,
		TicketID:  ticketID,
		Sessions:  make(map[string]string, len(list)),
		Tasks:     make(map[string]components.ReviewTask, len(tickets)),
		Documents: make(map[string]string, len(docs)),
	}
	for _, s := range list {
		title := s.Task
		if title == "" {
			title = "Untitled session"
		}
		seed.Sessions[s.ID] = sessions.RunsAs(s, names) + " · " + title
	}
	for _, t := range tickets {
		seed.Tasks[t.ID] = components.ReviewTask{Title: t.Title, Href: Href(projectID, t.ID)}
	}
	for _, d := range docs {
		seed.Documents[d.ID] = d.Title
	}
	return seed
}

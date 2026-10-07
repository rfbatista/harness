package tasks

import (
	"operators-mcp/internal/adapter/in/web/components"
	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/domain"
)

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

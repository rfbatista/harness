package web

import (
	"context"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/infra/readshare"
	"operators-mcp/internal/ports"
)

// A page and its layout read the same lists: the project's tickets for the
// page and the rail, the project's sessions for the page, the rail and the
// board counts. The ticket and session readers every page is handed make
// those one read each per request, shared through the request's readshare
// scope (see server.page).

// sharedTickets reads a project's tickets once per request.
type sharedTickets struct{ ports.TicketReader }

func (t sharedTickets) ListTickets(ctx context.Context, projectID string, filter ports.TicketFilter) ([]*domain.Ticket, error) {
	key := "tickets\x00" + projectID
	if filter.ParentID != nil {
		key += "\x00parent\x00" + *filter.ParentID
	}
	list, err := readshare.Do(ctx, key, func() ([]*domain.Ticket, error) {
		return t.TicketReader.ListTickets(ctx, projectID, filter)
	})
	return slices.Clone(list), err // each caller may sort its own copy
}

// sharedSessions reads a project's sessions once per request and answers a
// narrower filter within the project (a task's, an agent's, some statuses)
// from that list, in its order.
type sharedSessions struct{ ports.SessionReader }

func (s sharedSessions) List(ctx context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	if f.ProjectID == "" || len(f.TicketIDs) > 0 {
		return s.SessionReader.List(ctx, f)
	}
	all, err := readshare.Do(ctx, "sessions\x00"+f.ProjectID, func() ([]*domain.Session, error) {
		return s.SessionReader.List(ctx, ports.SessionFilter{ProjectID: f.ProjectID})
	})
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Session, 0, len(all))
	for _, sess := range all {
		if matches(sess, f) {
			out = append(out, sess)
		}
	}
	return out, nil
}

// matches reports whether sess, of f's project, passes f's other fields.
func matches(sess *domain.Session, f ports.SessionFilter) bool {
	return (f.TicketID == "" || sess.TicketID == f.TicketID) &&
		(f.AgentID == "" || sess.AgentID == f.AgentID) &&
		(len(f.Statuses) == 0 || slices.Contains(f.Statuses, sess.Status))
}

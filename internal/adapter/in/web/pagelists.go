package web

import (
	"context"
	"slices"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A page and its layout read the same lists: the project's tickets for the
// page and the rail, the project's sessions for the page, the rail and the
// board counts. pageLists makes those one read each per request. The page
// wraps every request's context with withPageLists, and the ticket and
// session readers every page is handed answer from it.

type pageListsKey struct{}

// pageLists are the lists one request has read, by project.
type pageLists struct {
	mu       sync.Mutex
	tickets  map[string]func() ([]*domain.Ticket, error)
	sessions map[string]func() ([]*domain.Session, error)
}

// withPageLists gives ctx's request its own shared lists.
func withPageLists(ctx context.Context) context.Context {
	return context.WithValue(ctx, pageListsKey{}, &pageLists{
		tickets:  map[string]func() ([]*domain.Ticket, error){},
		sessions: map[string]func() ([]*domain.Session, error){},
	})
}

func pageListsFrom(ctx context.Context) *pageLists {
	l, _ := ctx.Value(pageListsKey{}).(*pageLists)
	return l
}

// ticketsOf is the project's ticket list, read by load the first time it is
// asked for in the request.
func (l *pageLists) ticketsOf(projectID string, load func() ([]*domain.Ticket, error)) func() ([]*domain.Ticket, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	read, ok := l.tickets[projectID]
	if !ok {
		read = sync.OnceValues(load)
		l.tickets[projectID] = read
	}
	return read
}

// sessionsOf is the project's session list, read by load the first time it
// is asked for in the request.
func (l *pageLists) sessionsOf(projectID string, load func() ([]*domain.Session, error)) func() ([]*domain.Session, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	read, ok := l.sessions[projectID]
	if !ok {
		read = sync.OnceValues(load)
		l.sessions[projectID] = read
	}
	return read
}

// sharedTickets reads a project's tickets once per request.
type sharedTickets struct{ ports.TicketReader }

func (t sharedTickets) ListTickets(ctx context.Context, projectID string) ([]*domain.Ticket, error) {
	l := pageListsFrom(ctx)
	if l == nil {
		return t.TicketReader.ListTickets(ctx, projectID)
	}
	list, err := l.ticketsOf(projectID, func() ([]*domain.Ticket, error) {
		return t.TicketReader.ListTickets(ctx, projectID)
	})()
	return slices.Clone(list), err // each caller may sort its own copy
}

// sharedSessions reads a project's sessions once per request and answers a
// narrower filter within the project (a task's, an agent's, some statuses)
// from that list, in its order.
type sharedSessions struct{ ports.SessionReader }

func (s sharedSessions) List(ctx context.Context, f ports.SessionFilter) ([]*domain.Session, error) {
	l := pageListsFrom(ctx)
	if l == nil || f.ProjectID == "" || len(f.TicketIDs) > 0 {
		return s.SessionReader.List(ctx, f)
	}
	all, err := l.sessionsOf(f.ProjectID, func() ([]*domain.Session, error) {
		return s.SessionReader.List(ctx, ports.SessionFilter{ProjectID: f.ProjectID})
	})()
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

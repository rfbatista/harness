package orchestration

import (
	"context"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// feedBuffer is how many changes a follower may fall behind before it is
// dropped and must reload.
const feedBuffer = 256

// quietEvents carry a session's conversation, not a change to its record, so
// they are not worth a change on the feed.
var quietEvents = map[string]bool{
	"output": true, "output_delta": true, "tool_use": true, "tool_result": true,
	"artifact": true, // a publish changes what the session made, not its record
	// The architect channel announces its own objects on the feed.
	"task_message": true, "review_request": true, "status_check": true, "task_status": true,
}

// feed fans a project's changes (sessions and tickets) out to its followers.
type feed struct {
	mu   sync.Mutex
	subs map[string]map[chan ports.SessionChange]struct{} // by project ID
}

// FollowProject follows projectID's sessions and tickets until ctx ends.
func (s *Service) FollowProject(ctx context.Context, projectID string) (<-chan ports.SessionChange, error) {
	if projectID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "project_id is required"}
	}
	c := make(chan ports.SessionChange, feedBuffer)
	s.feed.mu.Lock()
	if s.feed.subs == nil {
		s.feed.subs = map[string]map[chan ports.SessionChange]struct{}{}
	}
	if s.feed.subs[projectID] == nil {
		s.feed.subs[projectID] = map[chan ports.SessionChange]struct{}{}
	}
	s.feed.subs[projectID][c] = struct{}{}
	s.feed.mu.Unlock()

	go func() {
		<-ctx.Done()
		s.feed.drop(projectID, c)
	}()
	return c, nil
}

// drop removes and closes a follower; whoever removes it closes it, once.
func (f *feed) drop(projectID string, c chan ports.SessionChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.subs[projectID][c]; ok {
		delete(f.subs[projectID], c)
		close(c)
	}
}

// send delivers change to the followers of its project. A follower that
// cannot take it is dropped rather than waited for.
func (f *feed) send(change ports.SessionChange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid := change.ProjectID()
	for c := range f.subs[pid] {
		select {
		case c <- change:
		default:
			delete(f.subs[pid], c)
			close(c)
		}
	}
}

// notify puts the session's current record on the feed after an event that
// changed it.
func (s *Service) notify(sessionID, eventType string) {
	if quietEvents[eventType] {
		return
	}
	if sess := s.sessions.Get(sessionID); sess != nil {
		s.feed.send(ports.SessionChange{Session: s.withResumability(sess)})
	}
}

// AnnounceTicket puts a ticket change on the project feed, next to the session
// changes, exactly as planning applied it. Planning calls it after every
// create, update and delete (ports.TicketAnnouncer).
func (s *Service) AnnounceTicket(tk *domain.Ticket, deleted bool) {
	if tk == nil || tk.ProjectID == "" {
		return
	}
	s.feed.send(ports.ProjectChange{Ticket: tk, Deleted: deleted})
}

// AnnounceChange puts any change on its project's feed: the architect
// channel's messages, reviews, status checks and status moves
// (ports.ProjectChangeSink).
func (s *Service) AnnounceChange(change ports.ProjectChange) {
	if change.ProjectID() == "" {
		return
	}
	s.feed.send(change)
}

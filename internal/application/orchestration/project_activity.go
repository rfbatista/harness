package orchestration

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionActivityReader = (*Service)(nil)

// liveStatuses are the statuses of a session whose process may still act.
var liveStatuses = []domain.SessionStatus{
	domain.SessionStarting, domain.SessionRunning, domain.SessionIdle, domain.SessionThinking,
	domain.SessionWaitingApproval, domain.SessionPaused,
}

// SessionActivityByProject answers each project's live session count and
// newest session activity: what a project summary shows.
func (s *Service) SessionActivityByProject(_ context.Context) (map[string]ports.SessionActivity, error) {
	return s.sessions.ActivityByProject()
}

// LiveProjectSessions names the project's sessions that are not done, failed
// or stopped, with their agent's name: what refuses a project's delete.
func (s *Service) LiveProjectSessions(ctx context.Context, projectID string) ([]domain.ProjectSessionRef, error) {
	live := s.sessions.List(ports.SessionFilter{ProjectID: projectID, Statuses: liveStatuses})
	refs := make([]domain.ProjectSessionRef, 0, len(live))
	for _, sess := range live {
		refs = append(refs, domain.ProjectSessionRef{ID: sess.ID, TicketID: sess.TicketID, Agent: s.agentName(ctx, sess.AgentID)})
	}
	return refs, nil
}

// agentName is the agent's name, its id when the agent is gone, "" for none.
func (s *Service) agentName(ctx context.Context, agentID string) string {
	if agentID == "" || s.catalog.Agents == nil {
		return agentID
	}
	if a, err := s.catalog.Agents.GetAgent(ctx, agentID); err == nil && a != nil && a.Name != "" {
		return a.Name
	}
	return agentID
}

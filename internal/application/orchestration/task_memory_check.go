package orchestration

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"operators-mcp/internal/domain"
)

// TaskMemoryNotUpdatedCode and TaskMemoryNotUpdatedMessage are the notice a
// done declaration gets once when the session left progress.md as it found it.
// The declaration answer joins the messages and codes only its first refusal,
// so the message carries its own code.
const (
	TaskMemoryNotUpdatedCode    = "TASK_MEMORY_NOT_UPDATED"
	TaskMemoryNotUpdatedMessage = "TASK_MEMORY_NOT_UPDATED: progress.md has not changed since this session started. Update it (Done, Failed and why, Next step, Open questions) so the next session can continue, then declare done again. Declaring again without updating it is accepted and noted."

	// TaskMemoryOmittedEvent is recorded on a session's log when it declared
	// done twice without updating progress.md.
	TaskMemoryOmittedEvent = "task_memory_omitted"
)

// taskMemoryCheck holds, per session, the progress.md it started with and
// whether it was already asked to update it. A restart forgets both: a
// session without a snapshot is not checked.
type taskMemoryCheck struct {
	mu      sync.Mutex
	atStart map[string]string
	asked   map[string]bool
}

// rememberProgressAtStart keeps the progress.md a session starts with.
func (s *Service) rememberProgressAtStart(ctx context.Context, sessionID, taskID, repositoryID string) {
	view, err := s.TaskMemory.GetTaskMemory(ctx, taskID, repositoryID)
	if err != nil {
		return
	}
	c := &s.taskMemoryCheck
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.atStart == nil {
		c.atStart, c.asked = map[string]string{}, map[string]bool{}
	}
	for _, f := range view.Files {
		if f.Name == domain.TaskMemoryProgressFile {
			c.atStart[sessionID] = f.Content
		}
	}
	delete(c.asked, sessionID)
}

// AskToUpdateTaskMemory answers whether a done declaration is to be refused
// for the session's progress.md: true once, when it is unchanged since the
// start and the effective configuration requires an update. The next
// declaration is let through and the omission recorded on the session.
func (s *Service) AskToUpdateTaskMemory(ctx context.Context, sess *domain.Session) bool {
	if s.AgentSystem == nil || s.TaskMemory == nil || sess == nil || sess.RepositoryID == "" {
		return false
	}
	eff, err := s.AgentSystem.Effective(ctx, sess.ProjectID, sess.RepositoryID)
	if err != nil {
		slog.Warn("read the task memory setting", "session", sess.ID, "err", err)
		return false
	}
	if !eff.Learn.TaskMemory.RequireUpdate.Value {
		return false
	}
	c := &s.taskMemoryCheck
	c.mu.Lock()
	start, ok := c.atStart[sess.ID]
	c.mu.Unlock()
	if !ok {
		return false
	}
	// What the agent wrote on disk is read back before comparing.
	s.syncTaskMemory(ctx, sess.TicketID, sess.RepositoryID)
	view, err := s.TaskMemory.GetTaskMemory(ctx, sess.TicketID, sess.RepositoryID)
	if err != nil {
		return false
	}
	now := ""
	for _, f := range view.Files {
		if f.Name == domain.TaskMemoryProgressFile {
			now = f.Content
		}
	}
	if !domain.ProgressUnchanged(start, now) {
		return false
	}
	c.mu.Lock()
	asked := c.asked[sess.ID]
	c.asked[sess.ID] = true
	c.mu.Unlock()
	if !asked {
		return true
	}
	if !s.recordedTaskMemoryOmission(sess.ID) {
		s.record(sess.ID, SessionEvent{Type: TaskMemoryOmittedEvent, SessionID: sess.ID, Text: "declared done without updating progress.md", At: time.Now()}, sessionChange{})
	}
	return false
}

// recordedTaskMemoryOmission says whether the session's log has the omission.
func (s *Service) recordedTaskMemoryOmission(sessionID string) bool {
	events, err := s.sessions.ListEvents(sessionID, 0)
	if err != nil {
		return false
	}
	for _, ev := range events {
		if ev.Type == TaskMemoryOmittedEvent {
			return true
		}
	}
	return false
}

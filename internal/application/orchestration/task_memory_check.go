package orchestration

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
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

// taskMemoryCheck serialises the checks. What a session started with and was
// asked is kept in a file beside the task's memory folder (see statePath), so
// a resume or a restart keeps it.
type taskMemoryCheck struct{ mu sync.Mutex }

// checkState is what the check keeps of one session.
type checkState struct {
	// ProgressAtStart is progress.md as the session first found it.
	ProgressAtStart string `json:"progress_at_start"`
	// Asked says the session was asked once to update it; Omitted, that it
	// declared again without and the omission is recorded.
	Asked   bool `json:"asked"`
	Omitted bool `json:"omitted"`
}

// statePath is where a session's check state is kept: beside the task's
// folder, outside what a session can write.
func (s *Service) statePath(taskID, repositoryID, sessionID string) string {
	dir := s.taskMemoryDir(taskID, repositoryID)
	if dir == "" {
		return ""
	}
	return dir + ".session-" + sessionID + ".json"
}

func readCheckState(path string) (checkState, bool) {
	var st checkState
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &st) != nil {
		return st, false
	}
	return st, true
}

func writeCheckState(path string, st checkState) {
	raw, err := json.Marshal(st)
	if err == nil {
		err = os.WriteFile(path, raw, 0o644)
	}
	if err != nil {
		slog.Warn("keep a session's task memory check state", "path", path, "err", err)
	}
}

// progressOf is the progress.md of a task memory view.
func progressOf(view *ports.TaskMemoryView) string {
	for _, f := range view.Files {
		if f.Name == domain.TaskMemoryProgressFile {
			return f.Content
		}
	}
	return ""
}

// rememberProgressAtStart keeps the progress.md a session starts with. A
// resume keeps the first snapshot: what the session wrote since is its update.
func (s *Service) rememberProgressAtStart(ctx context.Context, sessionID, taskID, repositoryID string) {
	path := s.statePath(taskID, repositoryID, sessionID)
	if path == "" {
		return
	}
	c := &s.taskMemoryCheck
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, kept := readCheckState(path); kept {
		return
	}
	view, err := s.TaskMemory.GetTaskMemory(ctx, taskID, repositoryID)
	if err != nil {
		return
	}
	writeCheckState(path, checkState{ProgressAtStart: progressOf(view)})
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
	path := s.statePath(sess.TicketID, sess.RepositoryID, sess.ID)
	if path == "" {
		return false
	}
	c := &s.taskMemoryCheck
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := readCheckState(path)
	if !ok {
		return false
	}
	// What the agent wrote on disk is read back before comparing.
	s.syncTaskMemory(ctx, sess.TicketID, sess.RepositoryID)
	view, err := s.TaskMemory.GetTaskMemory(ctx, sess.TicketID, sess.RepositoryID)
	if err != nil || !domain.ProgressUnchanged(st.ProgressAtStart, progressOf(view)) {
		return false
	}
	if !st.Asked {
		st.Asked = true
		writeCheckState(path, st)
		return true
	}
	if !st.Omitted {
		st.Omitted = true
		writeCheckState(path, st)
		s.record(sess.ID, SessionEvent{Type: TaskMemoryOmittedEvent, SessionID: sess.ID, Text: "declared done without updating progress.md", At: time.Now()}, sessionChange{})
	}
	return false
}

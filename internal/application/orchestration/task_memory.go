package orchestration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"operators-mcp/internal/domain"
)

// The task memory delivery (ADR 0013): the store is the source of truth; each
// task and repository has one folder under the harness's data area holding a
// delivery copy of plan.md and progress.md, writable by the sessions of the
// task. A sync reads back what a session wrote and refreshes what the person
// changed since; it runs at each start and turn end.

// taskMemorySync remembers, per delivered file, the content the harness last
// wrote there or read back from it, so a sync can tell who changed it.
type taskMemorySync struct {
	mu        sync.Mutex
	delivered map[string]string
}

// TaskMemoryDir is the delivery folder of a task in a repository, "" without
// a data area or a task memory store.
func (s *Service) taskMemoryDir(taskID, repositoryID string) string {
	if s.TaskMemory == nil || s.TaskMemoryRoot == "" || taskID == "" || repositoryID == "" {
		return ""
	}
	return filepath.Join(s.TaskMemoryRoot, taskID, repositoryID)
}

// syncTaskMemory brings the store and the delivery folder of a task in a
// repository in step, file by file: a file changed on disk since the last
// sync goes into the store (the last turn end wins); otherwise the store's
// copy goes to disk when it differs. It answers the folder, "" when there is
// none.
func (s *Service) syncTaskMemory(ctx context.Context, taskID, repositoryID string) string {
	dir := s.taskMemoryDir(taskID, repositoryID)
	if dir == "" {
		return ""
	}
	view, err := s.TaskMemory.GetTaskMemory(ctx, taskID, repositoryID)
	if err != nil {
		slog.Warn("read a task memory for delivery", "task", taskID, "repository", repositoryID, "err", err)
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("make a task memory folder", "dir", dir, "err", err)
		return ""
	}
	t := &s.taskMemorySync
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.delivered == nil {
		t.delivered = map[string]string{}
	}
	for _, f := range view.Files {
		p := filepath.Join(dir, f.Name)
		onDisk, readErr := os.ReadFile(p)
		last, known := t.delivered[p]
		if readErr == nil && known && string(onDisk) != last {
			if _, err := s.TaskMemory.SaveTaskMemory(ctx, taskID, repositoryID, f.Name, string(onDisk)); err != nil {
				slog.Warn("read a task memory file back", "path", p, "err", err)
				continue
			}
			t.delivered[p] = string(onDisk)
			continue
		}
		if readErr == nil && string(onDisk) == f.Content {
			t.delivered[p] = f.Content
			continue
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			slog.Warn("deliver a task memory file", "path", p, "err", err)
			continue
		}
		t.delivered[p] = f.Content
	}
	return dir
}

// deliverTaskMemory prepares the folder for a session starting on a task and
// answers it with the section of the brief that names the files.
func (s *Service) deliverTaskMemory(ctx context.Context, taskID, repositoryID string) (dir, section string) {
	dir = s.syncTaskMemory(ctx, taskID, repositoryID)
	if dir == "" {
		return "", ""
	}
	return dir, "## Task memory\n\nThe memory of this task in this repository is two files in " + dir + ":\n\n" +
		"- plan.md: the plan, one step per line marked todo, doing, done or failed.\n" +
		"- progress.md: the sections Done, Failed and why, Next step and Open questions.\n\n" +
		"Read both before you start: earlier sessions of this task wrote them. Keep them current as you work, " +
		"above all progress.md before you finish; the harness reads them back at the end of each turn and shares them with every session of this task."
}

// readBackTaskMemory is the turn end's sync for the session's task.
func (s *Service) readBackTaskMemory(ctx context.Context, sess *domain.Session) {
	if sess == nil {
		return
	}
	s.syncTaskMemory(ctx, sess.TicketID, sess.RepositoryID)
}

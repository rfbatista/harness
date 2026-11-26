// Package taskmemory is the task memory (ADR 0013): plan.md and progress.md
// of each task and repository, stored by the harness and shared by every
// session of the task in that repository. The person reads and edits it here;
// sessions get it delivered as files and what they wrote is saved back here.
package taskmemory

import (
	"context"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TaskMemories = (*Service)(nil)

// Service keeps the task memories.
type Service struct {
	store ports.TaskMemoryStore
	repos ports.RepositoryReader
	tasks ports.TicketReader
	feed  ports.ProjectChangeSink
}

// NewService returns a Service over its store.
func NewService(store ports.TaskMemoryStore, repos ports.RepositoryReader, tasks ports.TicketReader) *Service {
	return &Service{store: store, repos: repos, tasks: tasks}
}

// UseTasks sets where a task is read, for its project.
func (s *Service) UseTasks(t ports.TicketReader) { s.tasks = t }

// UseFeed sets where a change of a memory file is announced.
func (s *Service) UseFeed(f ports.ProjectChangeSink) { s.feed = f }

// GetTaskMemory implements ports.TaskMemories.
func (s *Service) GetTaskMemory(ctx context.Context, taskID, repositoryID string) (*ports.TaskMemoryView, error) {
	if _, err := s.check(ctx, taskID, repositoryID); err != nil {
		return nil, err
	}
	return s.view(ctx, taskID, repositoryID)
}

// SaveTaskMemory implements ports.TaskMemories.
func (s *Service) SaveTaskMemory(ctx context.Context, taskID, repositoryID, file, content string) (*ports.TaskMemoryView, error) {
	task, err := s.check(ctx, taskID, repositoryID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(domain.TaskMemoryFiles(), file) {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "file must be plan.md or progress.md"}
	}
	if err := s.store.PutTaskMemory(ctx, taskID, repositoryID, file, content); err != nil {
		return nil, err
	}
	if s.feed != nil {
		s.feed.AnnounceChange(ports.ProjectChange{TaskMemoryChanged: &ports.TaskMemoryChange{TaskID: taskID, RepositoryID: repositoryID, File: file, ProjectID: task.ProjectID}})
	}
	return s.view(ctx, taskID, repositoryID)
}

// check answers the task, or TICKET_NOT_FOUND / REPOSITORY_NOT_FOUND.
func (s *Service) check(ctx context.Context, taskID, repositoryID string) (*domain.Ticket, error) {
	task, err := s.tasks.GetTicket(ctx, taskID)
	if err != nil {
		return nil, err
	}
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if repo.ProjectID != task.ProjectID {
		// A memory is a task's in a repository of its own project.
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found in the task's project"}
	}
	return task, nil
}

func (s *Service) view(ctx context.Context, taskID, repositoryID string) (*ports.TaskMemoryView, error) {
	rows, err := s.store.ListTaskMemory(ctx, taskID, repositoryID)
	if err != nil {
		return nil, err
	}
	starter := domain.NewTaskMemory()
	out := &ports.TaskMemoryView{TaskID: taskID, RepositoryID: repositoryID}
	for _, name := range domain.TaskMemoryFiles() {
		f := ports.TaskMemoryFile{Name: name, Content: starter.Content(name)}
		for _, r := range rows {
			if r.File == name {
				t := r.UpdatedAt
				f.Content, f.UpdatedAt = r.Content, &t
			}
		}
		out.Files = append(out.Files, f)
	}
	return out, nil
}

package productowner

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The product owner's memory (ADR 0111): entries a project keeps across
// threads. The product owner reaches them through tools, each call made from
// a thread's session.

var (
	_ ports.ProductOwnerMemoryTools = (*Service)(nil)
	_ ports.ProductOwnerMemory      = (*Service)(nil)
)

// threadOfSession is the thread a tool call comes from.
func (s *Service) threadOfSession(ctx context.Context, sessionID string) (*domain.ProductOwnerThread, error) {
	t, err := s.store.ThreadOfSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "SESSION_NOT_FOUND: no product owner thread answers for this session"}
	}
	return t, nil
}

// memoryView shows an entry with the title of the thread it came from; a
// thread that cannot be read leaves the title empty.
func (s *Service) memoryView(ctx context.Context, e domain.MemoryEntry) ports.MemoryEntryView {
	v := ports.MemoryEntryView{EntryID: e.ID, Text: e.Text, SavedAt: e.SavedAt, ChangedAt: e.ChangedAt, ThreadID: e.ThreadID}
	if e.ChangedBy != "" {
		by := e.ChangedBy
		v.ChangedBy = &by
	}
	if t, err := s.store.Thread(ctx, e.ThreadID); err == nil {
		v.ThreadTitle = t.Title
	}
	return v
}

// ownEntry is the project's entry id, or MEMORY_ENTRY_NOT_FOUND when the id
// is unknown or belongs to another project.
func (s *Service) ownEntry(ctx context.Context, projectID, id string) (*domain.MemoryEntry, error) {
	e, err := s.store.MemoryEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if e.ProjectID != projectID {
		return nil, &domain.StructuredError{Code: "MEMORY_ENTRY_NOT_FOUND", Message: "MEMORY_ENTRY_NOT_FOUND: no memory entry " + id + " in this project: use an id from list_memory"}
	}
	return e, nil
}

// ListMemory implements ports.ProductOwnerMemoryTools.
func (s *Service) ListMemory(ctx context.Context, sessionID string) ([]ports.MemoryEntryView, error) {
	t, err := s.threadOfSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	entries, err := s.store.MemoryEntries(ctx, t.ProjectID)
	if err != nil {
		return nil, err
	}
	out := make([]ports.MemoryEntryView, len(entries))
	for i, e := range entries {
		out[i] = s.memoryView(ctx, e)
	}
	return out, nil
}

// SaveMemory implements ports.ProductOwnerMemoryTools.
func (s *Service) SaveMemory(ctx context.Context, sessionID, text string) (*ports.MemoryEntryView, error) {
	t, err := s.threadOfSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	text, err = domain.ValidMemoryText(text)
	if err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e := domain.MemoryEntry{ID: id, ProjectID: t.ProjectID, Text: text, SavedAt: now, ThreadID: t.ID}
	return s.applyByProductOwner(ctx, t, e, domain.MemoryOpSaved)
}

// ChangeMemory implements ports.ProductOwnerMemoryTools.
func (s *Service) ChangeMemory(ctx context.Context, sessionID, entryID, text string) (*ports.MemoryEntryView, error) {
	t, err := s.threadOfSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	text, err = domain.ValidMemoryText(text)
	if err != nil {
		return nil, err
	}
	s.memMu.Lock()
	defer s.memMu.Unlock()
	e, err := s.ownEntry(ctx, t.ProjectID, entryID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e.Text, e.ChangedAt, e.ChangedBy = text, &now, domain.MemoryByProductOwner
	return s.applyByProductOwnerLocked(ctx, t, *e, domain.MemoryOpChanged)
}

// DropMemory implements ports.ProductOwnerMemoryTools.
func (s *Service) DropMemory(ctx context.Context, sessionID, entryID string) error {
	t, err := s.threadOfSession(ctx, sessionID)
	if err != nil {
		return err
	}
	s.memMu.Lock()
	defer s.memMu.Unlock()
	e, err := s.ownEntry(ctx, t.ProjectID, entryID)
	if err != nil {
		return err
	}
	_, err = s.applyByProductOwnerLocked(ctx, t, *e, domain.MemoryOpDropped)
	return err
}

func (s *Service) applyByProductOwner(ctx context.Context, t *domain.ProductOwnerThread, e domain.MemoryEntry, op string) (*ports.MemoryEntryView, error) {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	return s.applyByProductOwnerLocked(ctx, t, e, op)
}

// applyByProductOwnerLocked stores the change, tells the thread with a
// memory note and the feed with a memory event. The caller holds memMu.
func (s *Service) applyByProductOwnerLocked(ctx context.Context, t *domain.ProductOwnerThread, e domain.MemoryEntry, op string) (*ports.MemoryEntryView, error) {
	change := domain.MemoryChange{ProjectID: e.ProjectID, EntryID: e.ID, Op: op, Text: e.Text, By: domain.MemoryByProductOwner, ThreadID: t.ID, At: time.Now()}
	if _, err := s.store.ApplyMemoryChange(ctx, e, change); err != nil {
		return nil, err
	}
	if item, err := s.store.AppendItem(ctx, domain.ProductOwnerItem{ThreadID: t.ID, Kind: domain.ProductOwnerItemMemoryNote, At: change.At,
		Op: op, EntryID: e.ID, Text: e.Text}); err == nil {
		s.announceItem(t, item)
	}
	s.announceMemory(e.ProjectID, e.ID, op)
	v := s.memoryView(ctx, e)
	return &v, nil
}

func (s *Service) announceMemory(projectID, entryID, op string) {
	s.announce(ports.ProjectChange{ProductOwnerMemory: &ports.ProductOwnerMemoryChange{ProjectID: projectID, EntryID: entryID, Op: op}})
}

// Memory implements ports.ProductOwnerMemory.
func (s *Service) Memory(ctx context.Context, projectID string) ([]ports.MemoryEntryView, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	entries, err := s.store.MemoryEntries(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ports.MemoryEntryView, len(entries))
	for i, e := range entries {
		out[i] = s.memoryView(ctx, e)
	}
	return out, nil
}

// UpdateMemoryEntry implements ports.ProductOwnerMemory. A refused update
// changes nothing.
func (s *Service) UpdateMemoryEntry(ctx context.Context, entryID, text string) (*ports.MemoryEntryView, error) {
	text, err := domain.ValidMemoryText(text)
	if err != nil {
		return nil, err
	}
	s.memMu.Lock()
	defer s.memMu.Unlock()
	e, err := s.store.MemoryEntry(ctx, entryID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	e.Text, e.ChangedAt, e.ChangedBy = text, &now, domain.MemoryByPerson
	return s.applyByPerson(ctx, *e, domain.MemoryOpChanged)
}

// DeleteMemoryEntry implements ports.ProductOwnerMemory.
func (s *Service) DeleteMemoryEntry(ctx context.Context, entryID string) error {
	s.memMu.Lock()
	defer s.memMu.Unlock()
	e, err := s.store.MemoryEntry(ctx, entryID)
	if err != nil {
		return err
	}
	_, err = s.applyByPerson(ctx, *e, domain.MemoryOpDropped)
	return err
}

// applyByPerson stores the person's change and tells the feed. It adds no
// thread item: a thread learns of it from the note on its next message.
func (s *Service) applyByPerson(ctx context.Context, e domain.MemoryEntry, op string) (*ports.MemoryEntryView, error) {
	change := domain.MemoryChange{ProjectID: e.ProjectID, EntryID: e.ID, Op: op, Text: e.Text, By: domain.MemoryByPerson, At: time.Now()}
	if _, err := s.store.ApplyMemoryChange(ctx, e, change); err != nil {
		return nil, err
	}
	s.announceMemory(e.ProjectID, e.ID, op)
	v := s.memoryView(ctx, e)
	return &v, nil
}

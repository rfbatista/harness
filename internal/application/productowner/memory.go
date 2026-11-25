package productowner

import (
	"context"
	"fmt"
	"strings"
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

func (s *Service) memoryViews(ctx context.Context, entries []domain.MemoryEntry) []ports.MemoryEntryView {
	out := make([]ports.MemoryEntryView, len(entries))
	for i, e := range entries {
		out[i] = s.memoryView(ctx, e)
	}
	return out
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
	return s.memoryViews(ctx, entries), nil
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
	return s.memoryViews(ctx, entries), nil
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

// OpeningMemory is the text that hands a project's whole memory to a thread:
// a thread's first message, and a thread continuing after a checkpoint, start
// with it (ADR 0111). It answers the memory version it describes, and no text
// when the project has no entries.
func (s *Service) OpeningMemory(ctx context.Context, projectID string) (text string, version int64, err error) {
	// The version is read first: a change that lands after it is told by the
	// next message's note, so none is missed.
	if version, err = s.store.MemoryVersion(ctx, projectID); err != nil {
		return "", 0, err
	}
	entries, err := s.store.MemoryEntries(ctx, projectID)
	if err != nil || len(entries) == 0 {
		return "", version, err
	}
	var b strings.Builder
	noun := "entries"
	if len(entries) == 1 {
		noun = "entry"
	}
	fmt.Fprintf(&b, "[Memory of this project: %d %s that you and the person agreed on. Rely on them, and say that you remember when it helps. Each starts with its id, which change_memory and drop_memory take.]\n", len(entries), noun)
	// Oldest first, so the list reads in the order things were agreed.
	for i := len(entries) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "- (%s) %s\n", entries[i].ID, entries[i].Text)
	}
	b.WriteString("[End of memory]")
	return b.String(), version, nil
}

// withMemory is what claude is handed for the person's message: the message
// itself, led by the whole memory when the thread has not been given it.
// version is what the thread has seen once the message is delivered.
func (s *Service) withMemory(ctx context.Context, t *domain.ProductOwnerThread, message string) (input string, version int64) {
	if !t.MemoryOpened {
		text, v, err := s.OpeningMemory(ctx, t.ProjectID)
		if err != nil {
			return message, t.MemorySeen
		}
		if text != "" {
			message = text + "\n\n" + message
		}
		return message, v
	}
	note, v, err := s.memoryNote(ctx, t)
	if err != nil {
		return message, t.MemorySeen
	}
	if note != "" {
		message = note + "\n\n" + message
	}
	return message, v
}

// memoryNote tells a thread what changed in its project's memory since it
// last saw it: the person's edits and deletes, and what the product owner
// saved in other threads. It never repeats an entry that did not change, and
// says nothing of a thread's own tool calls. version is what the thread has
// seen once the note is delivered.
func (s *Service) memoryNote(ctx context.Context, t *domain.ProductOwnerThread) (note string, version int64, err error) {
	version = t.MemorySeen
	changes, err := s.store.MemoryChangesSince(ctx, t.ProjectID, t.MemorySeen)
	if err != nil || len(changes) == 0 {
		return "", version, err
	}
	version = changes[len(changes)-1].Version
	type span struct{ first, last domain.MemoryChange }
	var order []string
	byEntry := map[string]*span{}
	for _, c := range changes {
		if n := byEntry[c.EntryID]; n != nil {
			// The thread's own later change still decides what the entry reads now.
			n.last = c
			continue
		}
		if c.By == domain.MemoryByProductOwner && c.ThreadID == t.ID {
			continue
		}
		byEntry[c.EntryID] = &span{first: c, last: c}
		order = append(order, c.EntryID)
	}
	var lines []string
	for _, id := range order {
		n := byEntry[id]
		switch {
		case n.last.Op == domain.MemoryOpDropped && n.first.Op == domain.MemoryOpSaved:
			// Saved and dropped while the thread was away: it never knew it.
		case n.last.Op == domain.MemoryOpDropped:
			lines = append(lines, fmt.Sprintf("- (%s) was deleted; do not rely on it any more: %s", id, n.last.Text))
		case n.first.Op == domain.MemoryOpSaved:
			lines = append(lines, fmt.Sprintf("- (%s) is a new entry: %s", id, n.last.Text))
		default:
			lines = append(lines, fmt.Sprintf("- (%s) now reads: %s", id, n.last.Text))
		}
	}
	if len(lines) == 0 {
		return "", version, nil
	}
	return "[Memory changed since you last looked. Entries not listed are as you know them.]\n" + strings.Join(lines, "\n") + "\n[End of changes]", version, nil
}

// sawMemory records the memory version a thread was handed.
func (s *Service) sawMemory(ctx context.Context, t *domain.ProductOwnerThread, version int64) {
	_, _ = s.update(ctx, t.ID, t, func(th *domain.ProductOwnerThread) bool {
		if th.MemoryOpened && th.MemorySeen >= version {
			return false
		}
		th.MemoryOpened, th.MemorySeen = true, version
		return true
	})
}

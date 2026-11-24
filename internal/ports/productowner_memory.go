package ports

import (
	"context"
	"time"
)

// MemoryEntryView is a memory entry as the contract and the product owner's
// tools show it (ADR 0111).
type MemoryEntryView struct {
	EntryID     string     `json:"entry_id"`
	Text        string     `json:"text"`
	SavedAt     time.Time  `json:"saved_at"`
	ChangedAt   *time.Time `json:"changed_at"`
	ChangedBy   *string    `json:"changed_by"`
	ThreadID    string     `json:"thread_id"`
	ThreadTitle string     `json:"thread_title"`
}

// ProductOwnerMemoryChange is a memory entry saved, changed or dropped: the
// page re-reads the entry list.
type ProductOwnerMemoryChange struct {
	ProjectID string `json:"project_id"`
	EntryID   string `json:"entry_id"`
	// Op is "saved", "changed" or "dropped".
	Op string `json:"op"`
}

// ProductOwnerMemoryTools is what the product owner's memory tools drive. Each
// call names the session it comes from: the thread of that session gives the
// project the call acts on, and the entry's source.
type ProductOwnerMemoryTools interface {
	ListMemory(ctx context.Context, sessionID string) ([]MemoryEntryView, error)
	SaveMemory(ctx context.Context, sessionID, text string) (*MemoryEntryView, error)
	ChangeMemory(ctx context.Context, sessionID, entryID, text string) (*MemoryEntryView, error)
	DropMemory(ctx context.Context, sessionID, entryID string) error
}

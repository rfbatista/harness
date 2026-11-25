package domain

import (
	"strings"
	"time"
)

// A project's product owner keeps memory entries, one durable fact each (ADR
// 0111). The entries and the log of their changes are a harness store per
// project.

// ProductOwnerMaxMemory bounds the text of one memory entry, in characters.
const ProductOwnerMaxMemory = 4000

// Who made a memory change.
const (
	MemoryByPerson       = "person"
	MemoryByProductOwner = "product_owner"
)

// Memory operations, as a thread's memory_note item and the memory event
// name them.
const (
	MemoryOpSaved   = "saved"
	MemoryOpChanged = "changed"
	MemoryOpDropped = "dropped"
)

// MemoryEntry is one durable fact of a project.
type MemoryEntry struct {
	ID        string
	ProjectID string
	Text      string
	SavedAt   time.Time
	// ChangedAt and ChangedBy are unset until the entry is first changed.
	ChangedAt *time.Time
	ChangedBy string
	// ThreadID is the thread the entry was saved from.
	ThreadID string
}

// MemoryChange is one line of a project's memory log: what happened to an
// entry, and who did it. Version orders the project's changes; it is the
// number a thread's "last saw" is kept as.
type MemoryChange struct {
	Version   int64
	ProjectID string
	EntryID   string
	Op        string
	// Text is the entry's text after the change, and the last one it had for a drop.
	Text string
	By   string
	// ThreadID is the thread that made a change of the product owner's; empty
	// for the person's.
	ThreadID string
	At       time.Time
}

// ValidMemoryText is the text trimmed, or INVALID_INPUT when it is empty or
// over ProductOwnerMaxMemory characters.
func ValidMemoryText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "INVALID_INPUT: the memory text is empty"}
	}
	if len([]rune(text)) > ProductOwnerMaxMemory {
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "INVALID_INPUT: the memory text is over 4,000 characters; shorten it or split it in two entries"}
	}
	return text, nil
}

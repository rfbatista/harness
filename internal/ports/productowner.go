package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// ProductOwnerStore keeps each project's product owner settings and the items
// of its threads. A thread's items are kept here, apart from the claude
// transcript, so a thread reads the same after resumes (ADR 0109).
type ProductOwnerStore interface {
	// Settings answers the project's settings; a project with none stored has
	// the defaults.
	Settings(ctx context.Context, projectID string) (domain.ProductOwnerSettings, error)
	SaveSettings(ctx context.Context, s domain.ProductOwnerSettings) error

	CreateThread(ctx context.Context, t domain.ProductOwnerThread) error
	// Thread answers THREAD_NOT_FOUND for an unknown id.
	Thread(ctx context.Context, id string) (*domain.ProductOwnerThread, error)
	// ThreadOfSession is the thread a session answers for, nil when none.
	ThreadOfSession(ctx context.Context, sessionID string) (*domain.ProductOwnerThread, error)
	// Threads are the project's, the most recently active first.
	Threads(ctx context.Context, projectID string) ([]domain.ProductOwnerThread, error)
	// ThreadsWithTurn are every project's threads in turn state.
	ThreadsWithTurn(ctx context.Context, turn string) ([]domain.ProductOwnerThread, error)
	SaveThread(ctx context.Context, t domain.ProductOwnerThread) error

	// AppendItem stores a new item last in its thread, giving it an id and a
	// sequence number, and answers it.
	AppendItem(ctx context.Context, item domain.ProductOwnerItem) (domain.ProductOwnerItem, error)
	SaveItem(ctx context.Context, item domain.ProductOwnerItem) error
	// Items are a thread's items, oldest first: at most limit, the newest
	// ones before the item beforeID (the thread's end when empty). hasEarlier
	// says items exist before the first one answered.
	Items(ctx context.Context, threadID, beforeID string, limit int) (items []domain.ProductOwnerItem, hasEarlier bool, err error)
}

// ProductOwnerSettingsView are a project's settings as the person reads them.
type ProductOwnerSettingsView struct {
	ProjectID string `json:"project_id"`
	Enabled   bool   `json:"enabled"`
	// Agent is nil while no catalog agent is chosen.
	Agent  *ProductOwnerAgent `json:"agent"`
	Prompt string             `json:"prompt"`
	// DigestTime is nil while no digest time is set.
	DigestTime *string `json:"digest_time"`
	// TimeZone is the server's local time zone, and read-only.
	TimeZone string `json:"time_zone"`
}

// ProductOwnerAgent is the catalog agent a product owner runs as.
type ProductOwnerAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProductOwnerSettingsUpdate changes only the fields that are set.
type ProductOwnerSettingsUpdate struct {
	ProjectID  string
	Enabled    *bool
	AgentID    *string
	Prompt     *string
	DigestTime *string
}

// ProductOwnerThreadPage is one read of a thread.
type ProductOwnerThreadPage struct {
	Thread     domain.ProductOwnerThread
	Items      []domain.ProductOwnerItem
	HasEarlier bool
}

// ProductOwner is what the web UI's API drives: a project's product owner
// settings and its threads.
type ProductOwner interface {
	Settings(ctx context.Context, projectID string) (*ProductOwnerSettingsView, error)
	UpdateSettings(ctx context.Context, u ProductOwnerSettingsUpdate) (*ProductOwnerSettingsView, error)

	Threads(ctx context.Context, projectID string) ([]domain.ProductOwnerThread, error)
	// Thread reads a thread and a page of its items; before pages toward the
	// start (an item id, empty for the newest).
	Thread(ctx context.Context, threadID, before string) (*ProductOwnerThreadPage, error)
	StartThread(ctx context.Context, projectID, message string, attachmentIDs []string) (threadID, itemID string, err error)
	SendMessage(ctx context.Context, threadID, message string, attachmentIDs []string) (itemID string, err error)
	StopTurn(ctx context.Context, threadID string) error
}

// The product owner's events on the project feed (the page stream): each
// names its project, which routes it.

// ProductOwnerThreadItemChange is a thread item, new or changed.
type ProductOwnerThreadItemChange struct {
	ProjectID string                  `json:"project_id"`
	ThreadID  string                  `json:"thread_id"`
	Item      domain.ProductOwnerItem `json:"item"`
}

// ProductOwnerDeltaChange is the next piece of a streaming answer; the final
// thread item is authoritative over the deltas.
type ProductOwnerDeltaChange struct {
	ProjectID string `json:"project_id"`
	ThreadID  string `json:"thread_id"`
	ItemID    string `json:"item_id"`
	Text      string `json:"text"`
}

// ProductOwnerThreadChange is a thread started or changed.
type ProductOwnerThreadChange struct {
	ProjectID string                    `json:"project_id"`
	Thread    domain.ProductOwnerThread `json:"thread"`
}

// ProductOwnerSettingsChange is a project's settings as they are now.
type ProductOwnerSettingsChange struct {
	ProjectID string                    `json:"project_id"`
	Settings  *ProductOwnerSettingsView `json:"settings"`
}

// ProductOwnerSessionSpec is what a thread's session starts with: the
// project's product owner settings at that time (ADR 0099). A resume passes
// the spec the thread started with.
type ProductOwnerSessionSpec struct {
	ProjectID string
	AgentID   string
	Prompt    string
}

// ProductOwnerSessions is how the product owner starts, resumes and drives the
// headless sessions of its threads; the orchestration implements it.
type ProductOwnerSessions interface {
	StartProductOwnerSession(ctx context.Context, spec ProductOwnerSessionSpec) (sessionID string, err error)
	ResumeProductOwnerSession(ctx context.Context, sessionID string, spec ProductOwnerSessionSpec) error
	ProductOwnerRunning(sessionID string) bool
	Send(ctx context.Context, id, text string) error
	Stop(ctx context.Context, id string) error
	// Subscribe follows a session's events live.
	Subscribe(id string) (<-chan SessionEvent, []SessionEvent, func())
}

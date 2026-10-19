package ports

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// The person's inbox across every project (see "Contract: Harness server ↔
// Web UI — notifications" and "Contract: Harness server ↔ Agent sessions —
// notification tools"). The notifications context owns it;
// *notifications.Service satisfies the driving ports.

// --- Driving ports ---

// NotificationQuery narrows a page of the inbox.
type NotificationQuery struct {
	// ProjectID narrows to one project; "" is every project.
	ProjectID  string
	UnreadOnly bool
	// Before is the opaque cursor a previous page answered as NextBefore.
	Before string
	// Since keeps only notifications created strictly after it; zero is no
	// bound.
	Since time.Time
	// Limit is 1..200; 0 is 50.
	Limit int
}

// NotificationPage is a page of the inbox, newest first. UnreadCount is the
// global unread count whatever the query; NextBefore is "" on the last page.
type NotificationPage struct {
	Notifications []*domain.Notification `json:"notifications"`
	UnreadCount   int                    `json:"unread_count"`
	NextBefore    string                 `json:"next_before,omitempty"`
}

// MarkNotificationsRead marks either the given ids or, with All, every unread
// notification (of ProjectID, when set). Exactly one of IDs and All.
type MarkNotificationsRead struct {
	IDs       []string `json:"ids,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	All       bool     `json:"all,omitempty"`
}

// NotificationChange is what one committed change did: the notifications it
// created, read or resolved, and the global unread count as of that commit.
// Changed is empty when notifications went with a deleted project.
type NotificationChange struct {
	Changed     []*domain.Notification `json:"changed"`
	UnreadCount int                    `json:"unread_count"`
}

// NotificationReader reads the inbox. The BFF seeds every page's unread count
// and latest notifications from it.
type NotificationReader interface {
	ListNotifications(ctx context.Context, q NotificationQuery) (*NotificationPage, error)
}

// Notifications is the person's inbox: read it, mark it read, follow it.
type Notifications interface {
	NotificationReader
	MarkNotificationsRead(ctx context.Context, req MarkNotificationsRead) (*NotificationChange, error)
	// FollowNotifications streams every committed change, in commit order,
	// until ctx ends. A follower that falls behind is closed and must reload.
	FollowNotifications(ctx context.Context) (<-chan NotificationChange, error)
}

// NotifyRequest is a notification an agent posts. SessionID is "" for an
// agent outside a harness session; TaskID is optional.
type NotifyRequest struct {
	SessionID string
	ProjectID string
	TaskID    string
	Title     string
	Body      string
	Level     domain.NotificationLevel // "" is info
	Link      string                   // "" is the task's page, or the project's board
}

// Notifier is what agents can do with the inbox: read it and post to it. It
// has no way to change read state, which belongs to the person.
type Notifier interface {
	NotificationReader
	Notify(ctx context.Context, req NotifyRequest) (*domain.Notification, error)
}

// --- Driven ports ---

// NotificationFilter narrows a repository listing, newest first.
type NotificationFilter struct {
	ProjectID  string
	UnreadOnly bool
	// BeforeAt and BeforeID are the keyset cursor: only notifications older
	// than (BeforeAt, BeforeID). Zero BeforeAt is no cursor.
	BeforeAt time.Time
	BeforeID string
	Since    time.Time
	Limit    int
}

// NotificationRepository persists notifications.
type NotificationRepository interface {
	// Create stores n with a fresh id.
	Create(n *domain.Notification) (*domain.Notification, error)
	Get(id string) *domain.Notification
	// OpenByCause is the unresolved notification with causeKey, or nil.
	OpenByCause(causeKey string) *domain.Notification
	// AnyByCause says whether any notification ever had causeKey.
	AnyByCause(causeKey string) bool
	List(ctx context.Context, f NotificationFilter) ([]*domain.Notification, error)
	CountUnread(ctx context.Context) (int, error)
	// MarkRead sets read_at on those of ids not read yet and answers them.
	MarkRead(ids []string, at time.Time) ([]*domain.Notification, error)
	// MarkAllRead sets read_at on every unread one (of projectID, when set).
	MarkAllRead(projectID string, at time.Time) ([]*domain.Notification, error)
	// Resolve settles the unresolved ones with causeKey and answers them.
	Resolve(causeKey string, at time.Time) ([]*domain.Notification, error)
	// ResolveTask settles every unresolved one of the task.
	ResolveTask(taskID string, at time.Time) ([]*domain.Notification, error)
	// CountAgentSince counts agent notifications created after since: the
	// session's, or with sessionID "", the project's posted without one.
	CountAgentSince(sessionID, projectID string, since time.Time) (int, error)
	DeleteByProject(projectID string) error
	// Prune deletes read or resolved notifications created before before.
	Prune(before time.Time) (int, error)
	// Transaction runs fn as one write transaction: what fn does through the
	// repository it is handed commits together, or not at all when fn
	// returns an error. Write transactions run one at a time, so they commit
	// in the order they run.
	Transaction(fn func(tx NotificationRepository) error) error
}

// NotificationDelivery hands newly recorded notifications to one outside
// channel: a log, later WhatsApp, email, Slack or a webhook. The inbox stays
// the source of truth; a provider only delivers. See "Contract: Notifications
// ↔ delivery providers".
type NotificationDelivery interface {
	// Name is the provider's stable name, [a-z][a-z0-9_-]*. It keys its
	// settings (notifications.<name>.enabled and .level) and its log lines.
	Name() string
	// Deliver sends one notification. It may block on the network but returns
	// when ctx ends. Nothing retries a failed delivery: a provider that wants
	// retries makes them inside Deliver, within ctx. Calls to one provider
	// are serial.
	Deliver(ctx context.Context, n domain.Notification) error
}

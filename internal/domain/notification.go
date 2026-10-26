package domain

import (
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// A notification is something the person should know or do, gathered across
// every project into one inbox. The server records them from what it sees
// (review requests, sessions waiting, tasks in review, failed sessions) and
// agents post their own. See "Contract: Harness server ↔ Web UI —
// notifications" and "Contract: Harness server ↔ Agent sessions —
// notification tools".

// NotificationKind is what caused a notification.
type NotificationKind string

const (
	NotificationReviewRequested NotificationKind = "review_requested"
	NotificationSessionWaiting  NotificationKind = "session_waiting"
	NotificationTaskReview      NotificationKind = "task_review"
	NotificationSessionFailed   NotificationKind = "session_failed"
	NotificationCommitCheckOff  NotificationKind = "commit_check_off"
	NotificationAgent           NotificationKind = "agent"
)

// NotificationLevel is how much a notification asks of the person: action
// means they have to do something.
type NotificationLevel string

const (
	NotificationInfo   NotificationLevel = "info"
	NotificationAction NotificationLevel = "action"
)

// NotificationSource says who recorded a notification.
type NotificationSource string

const (
	NotificationFromSystem NotificationSource = "system"
	NotificationFromAgent  NotificationSource = "agent"
)

const (
	// MaxNotificationTitleLen caps a title, in characters.
	MaxNotificationTitleLen = 120
	// MaxNotificationBodyLen caps a body, in characters.
	MaxNotificationBodyLen = 2000
)

// Notification is one item of the person's inbox. It is unread until the
// person reads it or its cause is settled.
type Notification struct {
	ID          string             `json:"id"`
	ProjectID   string             `json:"project_id"`
	ProjectName string             `json:"project_name"`
	TaskID      string             `json:"task_id,omitempty"`
	TaskTitle   string             `json:"task_title,omitempty"`
	SessionID   string             `json:"session_id,omitempty"`
	Source      NotificationSource `json:"source"`
	Kind        NotificationKind   `json:"kind"`
	Level       NotificationLevel  `json:"level"`
	Title       string             `json:"title"`
	Body        string             `json:"body,omitempty"`
	Link        string             `json:"link"`
	CreatedAt   time.Time          `json:"created_at"`
	ReadAt      *time.Time         `json:"read_at,omitempty"`
	ResolvedAt  *time.Time         `json:"resolved_at,omitempty"`
	// CauseKey names what caused it ("review:<id>", "session_waiting:<id>"…),
	// so a cause that repeats while unresolved adds nothing. Agent
	// notifications have none.
	CauseKey string `json:"-"`
}

// Unread says whether the person still has to see it.
func (n *Notification) Unread() bool { return n.ReadAt == nil && n.ResolvedAt == nil }

// ParseNotificationLevel accepts info or action.
func ParseNotificationLevel(s string) (NotificationLevel, error) {
	switch l := NotificationLevel(s); l {
	case NotificationInfo, NotificationAction:
		return l, nil
	}
	return "", invalidInput("level must be info or action")
}

// AtLeast says whether l is min or above it (action is above info).
func (l NotificationLevel) AtLeast(min NotificationLevel) bool {
	return min != NotificationAction || l == NotificationAction
}

// ValidateNotificationText checks a title (1–120 characters) and a body (at
// most 2 000), both already trimmed.
func ValidateNotificationText(title, body string) error {
	if title == "" {
		return invalidInput("title is required")
	}
	if utf8.RuneCountInString(title) > MaxNotificationTitleLen {
		return invalidInput("title is too long: at most 120 characters")
	}
	if utf8.RuneCountInString(body) > MaxNotificationBodyLen {
		return invalidInput("body is too long: at most 2000 characters")
	}
	return nil
}

// ValidateHarnessLink accepts a path on the harness web UI: it starts with
// "/", not "//", and has no scheme or host.
func ValidateHarnessLink(link string) error {
	bad := invalidInput(`link must be a harness path starting with "/", such as /projects/<project>/tasks/<task>`)
	if !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") || strings.ContainsAny(link, "\\\r\n\t") {
		return bad
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return bad
	}
	return nil
}

// ProjectLink is a project's board.
func ProjectLink(projectID string) string { return "/projects/" + url.PathEscape(projectID) }

// TaskLink is a task's page; with no task, its project's board.
func TaskLink(projectID, taskID string) string {
	if taskID == "" {
		return ProjectLink(projectID)
	}
	return ProjectLink(projectID) + "/tasks/" + url.PathEscape(taskID)
}

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
	NotificationBranchRequest   NotificationKind = "branch_request"
	NotificationBranchFinding   NotificationKind = "branch_finding"
	// NotificationServiceRequest is an agent's pending request to change a
	// service (ADR 0083).
	NotificationServiceRequest NotificationKind = "service_request"
	// NotificationSessionsInterrupted lists the sessions a restart interrupted
	// mid-turn, with a Resume all interrupted action (ADR 0083).
	NotificationSessionsInterrupted NotificationKind = "sessions_interrupted"
	NotificationAgent               NotificationKind = "agent"
)

// NotificationKinds is every kind, for filtering by one.
var NotificationKinds = []NotificationKind{
	NotificationReviewRequested, NotificationSessionWaiting, NotificationTaskReview, NotificationSessionFailed,
	NotificationCommitCheckOff, NotificationBranchRequest, NotificationBranchFinding, NotificationServiceRequest,
	NotificationSessionsInterrupted, NotificationAgent,
}

// NotificationActorKind says what kind of party caused a notification.
type NotificationActorKind string

const (
	ActorAgent  NotificationActorKind = "agent"
	ActorSystem NotificationActorKind = "system"
	ActorPerson NotificationActorKind = "person"
)

// The names of the harness's own sources, the actor name of a notification no
// session caused.
const (
	ActorBranchWatch = "branch watch"
	ActorRestart     = "restart"
	ActorHarness     = "harness"
	// ActorYou names the person: the harness keeps no display name for them.
	ActorYou = "you"
)

// NotificationActor is who caused a notification (ADR 0095): an agent with
// its session's mode word and session, or one of the harness's own sources.
type NotificationActor struct {
	Kind      NotificationActorKind `json:"kind"`
	Name      string                `json:"name"`
	Mode      string                `json:"mode,omitempty"`
	SessionID string                `json:"session_id,omitempty"`
}

// ModeWord is the session mode as a person says it: "architect",
// "implementing", "QA"; a session started with no mode is just a "session".
func ModeWord(m SessionMode) string {
	switch m {
	case SessionModeArchitect:
		return "architect"
	case SessionModeImplement:
		return "implementing"
	case SessionModeDesign:
		return "design"
	case SessionModeProduct:
		return "product"
	case SessionModeDesignSystem:
		return "design system"
	case SessionModeReview:
		return "review"
	case SessionModeTest:
		return "test"
	case SessionModeQA:
		return "QA"
	case SessionModeOperate:
		return "operate"
	}
	return "session"
}

// NotificationObject is what a notification's verb acts on: a branch, a
// command, a service (Code, shown monospace), a review subject, a task title.
type NotificationObject struct {
	Text string `json:"text"`
	Code bool   `json:"code"`
}

// CommitCheckState is where one session listed by a commit_check_off
// notification stands.
type CommitCheckState string

const (
	// CommitCheckOff: the session's commit check is still off.
	CommitCheckOff CommitCheckState = "off"
	// CommitCheckRepaired: the check is guarded again.
	CommitCheckRepaired CommitCheckState = "repaired"
	// CommitCheckEnded: the session was deleted.
	CommitCheckEnded CommitCheckState = "ended"
)

// ListedSession is one entry of a commit_check_off notification's list
// (ADR 0098). State and TaskTitle are as of the read.
type ListedSession struct {
	SessionID string           `json:"session_id"`
	TaskID    string           `json:"task_id,omitempty"`
	TaskTitle string           `json:"task_title,omitempty"`
	AgentName string           `json:"agent_name"`
	Link      string           `json:"link"`
	State     CommitCheckState `json:"state"`
}

// NotificationOutcome is how a notification's cause ended (ADR 0097). A
// notification resolved before outcomes were recorded has none.
type NotificationOutcome string

const (
	OutcomeApproved         NotificationOutcome = "approved"
	OutcomeChangesRequested NotificationOutcome = "changes_requested"
	OutcomeDenied           NotificationOutcome = "denied"
	OutcomeExpired          NotificationOutcome = "expired"
	OutcomeWithdrawn        NotificationOutcome = "withdrawn"
	OutcomeAnswered         NotificationOutcome = "answered"
	OutcomeDismissed        NotificationOutcome = "dismissed"
	OutcomeAcknowledged     NotificationOutcome = "acknowledged"
	OutcomeRepaired         NotificationOutcome = "repaired"
	OutcomeResumed          NotificationOutcome = "resumed"
	OutcomeEnded            NotificationOutcome = "ended"
	OutcomeDone             NotificationOutcome = "done"
	OutcomeReopened         NotificationOutcome = "reopened"
)

// Valid says o is one of the outcomes above.
func (o NotificationOutcome) Valid() bool {
	switch o {
	case OutcomeApproved, OutcomeChangesRequested, OutcomeDenied, OutcomeExpired, OutcomeWithdrawn, OutcomeAnswered,
		OutcomeDismissed, OutcomeAcknowledged, OutcomeRepaired, OutcomeResumed, OutcomeEnded, OutcomeDone, OutcomeReopened:
		return true
	}
	return false
}

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
	// Actor is who caused it: always set when the notification is read.
	Actor      NotificationActor `json:"actor"`
	CreatedAt  time.Time         `json:"created_at"`
	ReadAt     *time.Time        `json:"read_at,omitempty"`
	ResolvedAt *time.Time        `json:"resolved_at,omitempty"`
	// Verb and Object are what a system notification says, in the agent's
	// voice (ADR 0095); an agent's own notification has neither and its Title
	// is the what. Title still reads as the actor, the verb and the object in
	// plain text, for delivery providers.
	Verb   string              `json:"verb,omitempty"`
	Object *NotificationObject `json:"object,omitempty"`
	// RepositoryID is the repository a branch or service notification is
	// about; RepositoryName is its name as of the read.
	RepositoryID   string `json:"-"`
	RepositoryName string `json:"repository_name,omitempty"`
	// ListedSessions is the list of a commit_check_off notification.
	ListedSessions []ListedSession `json:"listed_sessions,omitempty"`
	// Outcome is how it ended; set with ResolvedAt, absent on rows resolved
	// before outcomes were recorded.
	Outcome NotificationOutcome `json:"outcome,omitempty"`
	// CauseKey names what caused it ("review:<id>", "session_waiting:<id>"…),
	// so a cause that repeats while unresolved adds nothing. Agent
	// notifications have none.
	CauseKey string `json:"-"`
	// InterruptedSessions is the list of a sessions_interrupted notification:
	// the sessions it lists, each with its state as of this read. Stored with
	// the notification: which sessions, and why the last resume of one failed.
	InterruptedSessions []InterruptedSession `json:"interrupted_sessions,omitempty"`
}

// InterruptedSessionState is where one listed session stands.
type InterruptedSessionState string

const (
	// InterruptedSessionInterrupted: the session still carries the mark.
	InterruptedSessionInterrupted InterruptedSessionState = "interrupted"
	// InterruptedSessionResumed: a resume cleared the mark.
	InterruptedSessionResumed InterruptedSessionState = "resumed"
	// InterruptedSessionFailed: the last Resume all could not resume it; it
	// still carries the mark.
	InterruptedSessionFailed InterruptedSessionState = "failed"
	// InterruptedSessionGone: the session was deleted.
	InterruptedSessionGone InterruptedSessionState = "gone"
)

// InterruptedSession is one entry of a sessions_interrupted notification's
// list (contract "Harness server ↔ Web UI — interrupted sessions and Resume
// all" §4). State and TaskTitle are filled when the notification is read.
type InterruptedSession struct {
	SessionID string                  `json:"session_id"`
	ProjectID string                  `json:"project_id"`
	TaskID    string                  `json:"task_id,omitempty"`
	TaskTitle string                  `json:"task_title,omitempty"`
	AgentName string                  `json:"agent_name"`
	Link      string                  `json:"link"`
	State     InterruptedSessionState `json:"state"`
	Reason    string                  `json:"reason,omitempty"`
}

// SessionsInterruptedCauseKey is the cause of every sessions_interrupted
// notification: only one is open at a time.
const SessionsInterruptedCauseKey = "sessions_interrupted"

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

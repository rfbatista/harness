package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The architect channel: a task's architect session coordinates the sessions
// it delegates. Delegates message it, it replies, it asks the person for
// reviews, and a status-check loop per delegate wakes it to check on them.

// SessionRole is what a session is on its task, derived from the task's
// sessions and never stored.
type SessionRole string

const (
	// RolePeer is a session outside the architect's tree, or on a task with no
	// architect: it was started by a person, or by a peer.
	RolePeer      SessionRole = ""
	RoleArchitect SessionRole = "architect"
	RoleDelegate  SessionRole = "delegate"
)

// TaskArchitect is the task's architect among sessions (all on one task): the
// most recently started session in architect mode, a running one beating an
// ended one. Nil when there is none.
func TaskArchitect(sessions []*Session) *Session {
	var best *Session
	for _, s := range sessions {
		if s == nil || s.Mode != SessionModeArchitect {
			continue
		}
		if best == nil || architectBeats(s, best) {
			best = s
		}
	}
	return best
}

func architectBeats(a, b *Session) bool {
	aLive, bLive := !a.Status.IsTerminal(), !b.Status.IsTerminal()
	if aLive != bLive {
		return aLive
	}
	return a.CreatedAt.After(b.CreatedAt)
}

// RoleOf is the role of session id among sessions (all on one task), and the
// task's architect id ("" when the task has none). A session is a delegate
// when its parent chain reaches the architect; a broken or cyclic chain makes
// it a peer.
func RoleOf(id string, sessions []*Session) (SessionRole, string) {
	arch := TaskArchitect(sessions)
	if arch == nil {
		return RolePeer, ""
	}
	if id == arch.ID {
		return RoleArchitect, arch.ID
	}
	byID := make(map[string]*Session, len(sessions))
	for _, s := range sessions {
		if s != nil {
			byID[s.ID] = s
		}
	}
	seen := map[string]bool{}
	for cur := byID[id]; cur != nil && !seen[cur.ID]; cur = byID[cur.ParentSessionID] {
		seen[cur.ID] = true
		if cur.ParentSessionID == arch.ID {
			return RoleDelegate, arch.ID
		}
	}
	return RolePeer, arch.ID
}

// TaskMessageKind is what a task message is for.
type TaskMessageKind string

const (
	MessageReviewRequest TaskMessageKind = "review_request"
	MessageStatusReport  TaskMessageKind = "status_report"
	MessageQuestion      TaskMessageKind = "question"
	MessageReply         TaskMessageKind = "reply"
)

// ReportStatus is where a delegate says it is, on a status_report.
type ReportStatus string

const (
	ReportWorking        ReportStatus = "working"
	ReportBlocked        ReportStatus = "blocked"
	ReportReadyForReview ReportStatus = "ready_for_review"
	ReportDone           ReportStatus = "done"
)

// Verdict is the architect's answer to a delegate's review_request.
type Verdict string

const (
	VerdictApproved         Verdict = "approved"
	VerdictChangesRequested Verdict = "changes_requested"
)

// TaskMessage is one message between a session and its task's architect. It
// outlives both sessions.
type TaskMessage struct {
	ID            string          `json:"id"`
	TaskID        string          `json:"task_id"`
	FromSessionID string          `json:"from_session_id"`
	ToSessionID   string          `json:"to_session_id"`
	Kind          TaskMessageKind `json:"kind"`
	Subject       string          `json:"subject,omitempty"`
	Body          string          `json:"body"`
	Status        ReportStatus    `json:"status,omitempty"`
	Verdict       Verdict         `json:"verdict,omitempty"`
	InReplyTo     string          `json:"in_reply_to,omitempty"`
	DocumentIDs   []string        `json:"document_ids"`
	ArtifactIDs   []string        `json:"artifact_ids"`
	Delivered     bool            `json:"delivered"`
	DeliveredAt   *time.Time      `json:"delivered_at,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	// ProjectID routes the message's feed events. Queued marks a message
	// waiting in its running recipient's outbox for the end of a turn. Both
	// are the server's own bookkeeping, not on the wire.
	ProjectID string `json:"-"`
	Queued    bool   `json:"-"`
}

// Validate checks the message's own fields: a known kind, a body, a status
// only on a status_report and a verdict only on a reply. Whether a verdict's
// reply answers a review_request is the caller's to check.
func (m *TaskMessage) Validate() error {
	switch m.Kind {
	case MessageReviewRequest, MessageStatusReport, MessageQuestion, MessageReply:
	default:
		return invalidInput("kind must be review_request, status_report, question or reply")
	}
	if strings.TrimSpace(m.Body) == "" {
		return invalidInput("body is required")
	}
	switch m.Status {
	case "":
	case ReportWorking, ReportBlocked, ReportReadyForReview, ReportDone:
		if m.Kind != MessageStatusReport {
			return invalidInput("status belongs on a status_report only")
		}
	default:
		return invalidInput("status must be working, blocked, ready_for_review or done")
	}
	switch m.Verdict {
	case "":
	case VerdictApproved, VerdictChangesRequested:
		if m.Kind != MessageReply {
			return invalidInput("verdict belongs on a reply only")
		}
	default:
		return invalidInput("verdict must be approved or changes_requested")
	}
	return nil
}

// ReviewState is where a review request the architect raised for the person
// stands.
type ReviewState string

const (
	ReviewPending          ReviewState = "pending"
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
	ReviewWithdrawn        ReviewState = "withdrawn"
)

// ParseReviewState reads a state filter; empty means any.
func ParseReviewState(s string) (ReviewState, error) {
	switch st := ReviewState(s); st {
	case "", ReviewPending, ReviewApproved, ReviewChangesRequested, ReviewWithdrawn:
		return st, nil
	}
	return "", invalidInput("state must be pending, approved, changes_requested or withdrawn")
}

// ReviewRequest is the architect asking the person to look at something. It
// stays pending until the person answers or the architect withdraws it.
type ReviewRequest struct {
	ID                 string      `json:"id"`
	TaskID             string      `json:"task_id"`
	ProjectID          string      `json:"project_id"`
	ArchitectSessionID string      `json:"architect_session_id"`
	AboutSessionID     string      `json:"about_session_id,omitempty"`
	Subject            string      `json:"subject"`
	Body               string      `json:"body"`
	DocumentIDs        []string    `json:"document_ids"`
	ArtifactIDs        []string    `json:"artifact_ids"`
	State              ReviewState `json:"state"`
	ResponseNote       string      `json:"response_note,omitempty"`
	RespondedAt        *time.Time  `json:"responded_at,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

// StatusCheckState is where a status-check loop stands.
type StatusCheckState string

const (
	StatusCheckActive StatusCheckState = "active"
	StatusCheckPaused StatusCheckState = "paused"
	StatusCheckEnded  StatusCheckState = "ended"
)

// Status-check intervals, in minutes.
const (
	StatusCheckDefaultMinutes = 10
	StatusCheckMinMinutes     = 2
	StatusCheckMaxMinutes     = 240
)

// ValidStatusCheckMinutes checks an interval: 0 (paused, or no loop) or
// within [StatusCheckMinMinutes, StatusCheckMaxMinutes].
func ValidStatusCheckMinutes(n int) error {
	if n == 0 || (n >= StatusCheckMinMinutes && n <= StatusCheckMaxMinutes) {
		return nil
	}
	return invalidInput("every_minutes must be 0, or between 2 and 240")
}

// StatusCheck is the recurring check on one delegate the architect started
// directly: each firing wakes the architect with the delegate's state.
type StatusCheck struct {
	TaskID             string           `json:"task_id"`
	ArchitectSessionID string           `json:"architect_session_id"`
	DelegateSessionID  string           `json:"delegate_session_id"`
	EveryMinutes       int              `json:"every_minutes"`
	NextAt             *time.Time       `json:"next_at,omitempty"`
	LastFiredAt        *time.Time       `json:"last_fired_at,omitempty"`
	FiredCount         int              `json:"fired_count"`
	State              StatusCheckState `json:"state"`
	// ProjectID routes the check's feed events; not part of the contract shape.
	ProjectID string `json:"-"`
}

// Every is the check's interval.
func (c *StatusCheck) Every() time.Duration { return time.Duration(c.EveryMinutes) * time.Minute }

// StatusChanger says who moved a task's status.
type StatusChanger string

const (
	ChangedBySession StatusChanger = "session"
	ChangedByPerson  StatusChanger = "person"
	// ChangedByArchitect is a move by the session that was the task's
	// architect when it moved it.
	ChangedByArchitect StatusChanger = "architect"
)

// MaxStatusReasonLen caps the reason given with a status change.
const MaxStatusReasonLen = 280

// TaskStatusChange is one move of a task's status, with who made it and why.
type TaskStatusChange struct {
	TaskID      string        `json:"task_id"`
	ProjectID   string        `json:"-"`
	Status      TicketStatus  `json:"status"`
	Reason      string        `json:"reason,omitempty"`
	BySessionID string        `json:"by_session_id,omitempty"`
	By          StatusChanger `json:"by"`
	At          time.Time     `json:"at"`
}

func invalidInput(msg string) error { return &StructuredError{Code: "INVALID_INPUT", Message: msg} }

// BringUnderSkip is why a session cannot be brought under its task's
// architect; empty means it can.
type BringUnderSkip string

const (
	SkipNotOnTask    BringUnderSkip = "not_on_task"
	SkipIsArchitect  BringUnderSkip = "is_architect"
	SkipAlreadyUnder BringUnderSkip = "already_under"
	SkipNotFound     BringUnderSkip = "not_found"
)

// BringUnderEligibility judges target (nil when it no longer exists) against
// the task ticketID, its architect arch and every session onTask. Any state
// qualifies: only where the session stands in the task decides. A nil arch
// is a task about to get its architect: nothing is the architect yet and no
// session is a delegate.
func BringUnderEligibility(ticketID string, arch *Session, onTask []*Session, target *Session) BringUnderSkip {
	switch {
	case target == nil:
		return SkipNotFound
	case target.TicketID != ticketID:
		return SkipNotOnTask
	case arch == nil:
		return ""
	case target.ID == arch.ID:
		return SkipIsArchitect
	}
	if role, _ := RoleOf(target.ID, onTask); role == RoleDelegate {
		return SkipAlreadyUnder
	}
	return ""
}

// DelegateStatusSection is the part of a delegate's brief about the task
// status and the architect; the turn a session brought under an architect
// gets repeats everything after its first line.
const DelegateStatusSection = `This task has an architect: the session that shaped it and delegated your part.
It owns the task status, and only the person moves it to done.
Do not call update_task_status; it is refused.
Talk to the architect instead:

- mcp__task__message_architect — kind review_request when your plan or work is ready for review (attach the documents and artifacts by id, from .index.json); kind status_report at each milestone, when you finish, and as soon as you are blocked (status working, blocked, ready_for_review or done); kind question when you need a decision
- mcp__task__list_task_messages — your messages with the architect, to catch up on a reply you missed

Its replies arrive as a new turn starting with [task message … · reply …]. A reply
with verdict changes_requested means: make the changes, then ask for review again.
Sessions you start on this task also report to the architect.`

// ShortAge is a duration as the turn headers print it: 12s, 7m or 3h.
func ShortAge(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
}

// BroughtUnderBriefClip is how many characters of a session's prompt the
// lists of brought-under sessions keep.
const BroughtUnderBriefClip = 120

// BroughtUnderLine is the line a turn gives a session brought under an
// architect: who it is, where it stands, what it was asked to do. agent names
// its agent; now dates its age.
func BroughtUnderLine(agent string, s *Session, now time.Time) string {
	return fmt.Sprintf("- %s session %s · status %s · branch %s · brief %q · last action %q · started %s ago",
		agent, s.ID, s.Status, s.Branch, clipLine(s.Task, BroughtUnderBriefClip), s.LastAction, ShortAge(now.Sub(s.CreatedAt)))
}

// clipLine is text on one line, cut to max runes with an ellipsis.
func clipLine(text string, max int) string {
	line := strings.Join(strings.Fields(text), " ")
	if r := []rune(line); len(r) > max {
		return string(r[:max]) + "…"
	}
	return line
}

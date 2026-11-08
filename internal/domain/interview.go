package domain

import "time"

// An interview is a set of questions a session's agent asks the person, kept
// with the person's answers. See "Contract: harness server ↔ web UI —
// interviews". The JSON tags are that contract's wire shape.

// InterviewState is where an interview stands: open until the person sends
// answers or dismisses it, or the session ends.
type InterviewState string

const (
	InterviewOpen      InterviewState = "open"
	InterviewSent      InterviewState = "sent"
	InterviewDismissed InterviewState = "dismissed"
	InterviewExpired   InterviewState = "expired"
)

// Closed says the interview left open.
func (s InterviewState) Closed() bool { return s != InterviewOpen }

// InterviewDelivery says whether the turn that carries a sent or dismissed
// interview reached the agent.
type InterviewDelivery string

const (
	InterviewDelivered InterviewDelivery = "delivered"
	InterviewQueued    InterviewDelivery = "queued"
)

// QuestionKind is how a question is answered.
type QuestionKind string

const (
	QuestionSingle   QuestionKind = "single"
	QuestionMultiple QuestionKind = "multiple"
	QuestionText     QuestionKind = "text"
)

// InterviewOption is one choice of a question; Description may be "".
type InterviewOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Recommended is the answer the agent suggests: an option's index or a text.
// Exactly one of the two is set.
type Recommended struct {
	Option *int    `json:"option,omitempty"`
	Text   *string `json:"text,omitempty"`
}

// InterviewQuestion is one question; Body is markdown. Recommended is nil
// when the agent suggests nothing.
type InterviewQuestion struct {
	Title       string            `json:"title"`
	Body        string            `json:"body"`
	Kind        QuestionKind      `json:"kind"`
	Options     []InterviewOption `json:"options"`
	Recommended *Recommended      `json:"recommended"`
}

// InterviewAnswer answers the question at its own index: the chosen option
// indexes (possibly none), a text, a note, or a skip.
type InterviewAnswer struct {
	Chosen  []int  `json:"chosen"`
	Text    string `json:"text"`
	Note    string `json:"note"`
	Skipped bool   `json:"skipped"`
}

// Interview is a session's questions and what became of them. EndedAt is nil
// while open. Delivery and DeliveredAt are set only on sent and dismissed
// ones, Answers only on sent ones, DismissNote only on dismissed ones.
type Interview struct {
	ID          string              `json:"id"`
	SessionID   string              `json:"session_id"`
	AgentName   string              `json:"agent_name"`
	Title       string              `json:"title"`
	AskedAt     time.Time           `json:"asked_at"`
	State       InterviewState      `json:"state"`
	EndedAt     *time.Time          `json:"ended_at"`
	Delivery    *InterviewDelivery  `json:"delivery"`
	DeliveredAt *time.Time          `json:"delivered_at"`
	Questions   []InterviewQuestion `json:"questions"`
	Answers     []InterviewAnswer   `json:"answers"`
	DismissNote *string             `json:"dismiss_note"`
}

// MaxClosedInterviews is how many closed interviews of a session are read.
const MaxClosedInterviews = 50

// InterviewList is a session's interviews as the seed and the resync read
// carry them: the open ones first, oldest first, then the closed ones, the
// last to end first. Seq is the last number its feed used; the list holds
// every change up to it. Interviews is never null.
type InterviewList struct {
	Seq        int64        `json:"seq"`
	Interviews []*Interview `json:"interviews"`
}

// InterviewEventKind is what an interview feed event reports.
type InterviewEventKind string

const (
	// InterviewAsked is a new open interview.
	InterviewAsked InterviewEventKind = "interview_asked"
	// InterviewChanged is a state or delivery change; the event carries the
	// whole interview.
	InterviewChanged InterviewEventKind = "interview_changed"
)

// InterviewEvent is one event of a session's interview feed. Seq counts up by
// one per session.
type InterviewEvent struct {
	Seq       int64              `json:"seq"`
	Kind      InterviewEventKind `json:"kind"`
	Interview *Interview         `json:"interview"`
}

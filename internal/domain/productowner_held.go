package domain

import "time"

// ProductOwnerItemHeldAction is the thread item for an action the product
// owner holds until the person answers (ADR 0110).
const ProductOwnerItemHeldAction = "held_action"

// States of a held action.
const (
	HeldActionPending   = "pending"
	HeldActionConfirmed = "confirmed"
	HeldActionDeclined  = "declined"
	HeldActionExpired   = "expired"
)

// Answers the person gives a held action.
const (
	HeldAnswerConfirm = "confirm"
	HeldAnswerDecline = "decline"
)

// HeldActionTTL is how long a held action waits for the person.
const HeldActionTTL = 24 * time.Hour

// Kinds of held action.
const (
	HeldActionArchiveTask = "archive_task"
	HeldActionChangeTask  = "change_task"
)

// HeldTarget is the task or document a held action is about, as the thread
// shows it.
type HeldTarget struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// HeldAction is what applies when the person confirms: the kind of change,
// the task, and for a change the fields to set. It is kept with the item and
// never on the wire.
type HeldAction struct {
	Kind        string  `json:"kind"`
	TaskID      string  `json:"task_id"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	// BaseTitle, BaseDescription and BaseStatus are the task's values when the
	// change was held. Confirming applies only while the task still has them.
	BaseTitle       string `json:"base_title,omitempty"`
	BaseDescription string `json:"base_description,omitempty"`
	BaseStatus      string `json:"base_status,omitempty"`
	// Told says the product owner was given the person's answer, on a turn.
	Told bool `json:"told,omitempty"`
}

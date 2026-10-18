package domain

// Event is a domain event: something that happened in one bounded context that
// another may have to react to. Events carry ids and values, never entities, so
// a subscriber reads what it needs through its own ports.
type Event interface {
	// EventName is the stable name subscribers register for.
	EventName() string
}

// ProjectDeleted: a project and its repositories are gone. Everything else
// scoped to the project must go with it.
type ProjectDeleted struct{ ProjectID string }

// RepositoryDeleted: one repository is gone (a deleted project announces
// ProjectDeleted instead). What is kept for it must go with it.
type RepositoryDeleted struct {
	RepositoryID string
	ProjectID    string
}

// PromptDeleted: a prompt is gone. Whatever referenced it drops the reference.
type PromptDeleted struct{ PromptID string }

// AgentDeleted: an agent is gone. Whatever referenced it drops the reference.
type AgentDeleted struct{ AgentID string }

// SkillDeleted: a skill is being deleted. It is published before the row is
// removed, and a subscriber error cancels the delete, so no reference to it
// can outlive it.
type SkillDeleted struct{ SkillID string }

// MCPServerDeleted: an MCP server is being deleted, with the same fail-closed
// ordering as SkillDeleted.
type MCPServerDeleted struct{ ServerID string }

// SessionDeleted: a session is being deleted. It is published before its
// worktree is removed, so whatever still runs there (an application run)
// stops first.
type SessionDeleted struct{ SessionID string }

// SettingsChanged: one setting now has a different value.
type SettingsChanged struct {
	Key string
	Old string
	New string
}

// SessionStarted: a session was recorded and is starting. StatusCheckMinutes
// is what its starter asked for the status-check loop (nil: the default).
type SessionStarted struct {
	SessionID          string
	ProjectID          string
	TicketID           string
	ParentSessionID    string
	Mode               SessionMode
	StatusCheckMinutes *int
}

// SessionEnded: a session's process is gone, with Status (done, failed or
// stopped).
type SessionEnded struct {
	SessionID string
	Status    SessionStatus
}

// SessionTurnEnded: a session finished a turn and is waiting for input.
type SessionTurnEnded struct{ SessionID string }

// TicketStatusChanged: a task's status moved.
type TicketStatusChanged struct{ Change TaskStatusChange }

// TicketDeleted: a task was deleted. It is published after the delete, so
// what other contexts kept about the task (attachments) can go too.
type TicketDeleted struct {
	TicketID  string
	ProjectID string
}

// ReviewRequestChanged: a review request for the person was opened, answered
// or withdrawn; State is where it now stands.
type ReviewRequestChanged struct {
	ReviewID       string
	TaskID         string
	ProjectID      string
	Subject        string
	AboutSessionID string
	State          ReviewState
}

// SessionStatusChanged: a session's recorded status moved to Status. It is
// announced on a real transition only, not on every event that restates it.
type SessionStatusChanged struct {
	SessionID string
	Status    SessionStatus
}

// TicketResourceAdded: a person added a resource to a task. Edits and
// deletes raise nothing: only an addition is news to the task's sessions.
type TicketResourceAdded struct{ Resource TicketResource }

// TicketCommentAdded: a person commented on a task.
type TicketCommentAdded struct{ Comment TicketComment }

// TokenSaverActivationChanged: one token saver's activation was set for an
// owner (Value inherit: its setting was removed). It is published after the
// change is stored.
type TokenSaverActivationChanged struct {
	Saver     TokenSaver
	Owner     ActivationOwner
	ProjectID string
	Value     ActivationValue
}

// TokenSaverInstallCompleted: a token saver was installed, or updated, at Version.
type TokenSaverInstallCompleted struct {
	Saver   TokenSaver
	Version string
}

// DocumentChange is what happened to a document.
type DocumentChange string

const (
	DocumentCreated      DocumentChange = "created"
	DocumentUpdated      DocumentChange = "updated"
	DocumentDeleted      DocumentChange = "deleted"
	DocumentScopeChanged DocumentChange = "scope_changed"
	DocumentLinked       DocumentChange = "linked"
	DocumentUnlinked     DocumentChange = "unlinked"
)

// DocumentChanged: a document was created, updated, deleted, moved between
// task and project scope, or linked to or unlinked from a task (TicketID).
// It is published after the change is stored, whoever made it.
type DocumentChanged struct {
	DocumentID string
	ProjectID  string
	TicketID   string
	Change     DocumentChange
}

// ArtifactChanged: an artifact was published, re-published, unpublished,
// deleted, moved between task and project scope, or attached to or detached
// from a task. It is published after the change is stored, whoever made it.
type ArtifactChanged struct {
	ArtifactID string
	ProjectID  string
}

func (ProjectDeleted) EventName() string       { return "project.deleted" }
func (RepositoryDeleted) EventName() string    { return "repository.deleted" }
func (PromptDeleted) EventName() string        { return "prompt.deleted" }
func (AgentDeleted) EventName() string         { return "agent.deleted" }
func (SkillDeleted) EventName() string         { return "skill.deleted" }
func (MCPServerDeleted) EventName() string     { return "mcp_server.deleted" }
func (SettingsChanged) EventName() string      { return "settings.changed" }
func (SessionDeleted) EventName() string       { return "session.deleted" }
func (SessionStarted) EventName() string       { return "session.started" }
func (SessionEnded) EventName() string         { return "session.ended" }
func (SessionTurnEnded) EventName() string     { return "session.turn_ended" }
func (TicketStatusChanged) EventName() string  { return "ticket.status_changed" }
func (TicketDeleted) EventName() string        { return "ticket.deleted" }
func (TicketResourceAdded) EventName() string  { return "ticket.resource_added" }
func (ReviewRequestChanged) EventName() string { return "review_request.changed" }
func (SessionStatusChanged) EventName() string { return "session.status_changed" }
func (TicketCommentAdded) EventName() string   { return "ticket.comment_added" }
func (DocumentChanged) EventName() string      { return "document.changed" }
func (ArtifactChanged) EventName() string      { return "artifact.changed" }

func (TokenSaverActivationChanged) EventName() string { return "token_saver.activation_changed" }
func (TokenSaverInstallCompleted) EventName() string  { return "token_saver.install_completed" }

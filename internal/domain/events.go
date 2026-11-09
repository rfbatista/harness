package domain

import "time"

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

// RepositorySaved: a repository was created or updated (its root dir may
// be new). It is published after the change is stored.
type RepositorySaved struct {
	RepositoryID string
	ProjectID    string
}

// RepositoryPromptsChanged: a repository prompt was saved or removed, or
// moved with its agent's rename. The virtual shells of the project's sessions
// show the prompt files.
type RepositoryPromptsChanged struct {
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

// InterviewOpened: a session's agent asked the person an interview, which was
// stored as open. It is published once per interview.
type InterviewOpened struct {
	InterviewID string
	SessionID   string
	Title       string
	Questions   int
}

// InterviewClosed: an interview left open: it was sent, dismissed or expired.
type InterviewClosed struct {
	InterviewID string
	SessionID   string
}

// SessionCommitCheckChanged: a session's commit check went off (Off, with the
// Reason) or was repaired. It is published at every check while the session
// stays off, so a subscriber deduplicates by session.
type SessionCommitCheckChanged struct {
	SessionID string
	ProjectID string
	TicketID  string
	Off       bool
	Reason    string
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
	// ByBranchRequest says the status moved because a branch request is
	// pending: the inbox has its own notification for that.
	ByBranchRequest bool
}

// BranchRequestChanged: a branch request was filed, answered, used or expired;
// Request is it as it is now.
type BranchRequestChanged struct {
	Request   BranchRequest
	ProjectID string
}

// SessionWorktreeProvisioned: a session's worktree is on disk, before its
// agent acts. The Branches list takes the session's starting snapshot then.
type SessionWorktreeProvisioned struct{ SessionID string }

// BranchFindingFound: the after-turn check found a branch or push no branch
// request explains (ADR 0070). SessionID is "" when no evidence names one
// session; Candidates are the sessions whose turns overlapped then.
type BranchFindingFound struct {
	ProjectID    string
	TaskID       string
	SessionID    string
	RepositoryID string
	Branch       string
	// Kind says what was found; Branch is the branch, or the remote's name for
	// a remote, and NewName the new name of a rename.
	Kind    BranchFindingKind
	NewName string
	// URL is the remote's URL, redacted, for a remote finding.
	URL        string
	Candidates []string
	FoundAt    time.Time
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

func (ProjectDeleted) EventName() string             { return "project.deleted" }
func (RepositoryDeleted) EventName() string          { return "repository.deleted" }
func (RepositorySaved) EventName() string            { return "repository.saved" }
func (RepositoryPromptsChanged) EventName() string   { return "repository.prompts_changed" }
func (PromptDeleted) EventName() string              { return "prompt.deleted" }
func (AgentDeleted) EventName() string               { return "agent.deleted" }
func (SkillDeleted) EventName() string               { return "skill.deleted" }
func (MCPServerDeleted) EventName() string           { return "mcp_server.deleted" }
func (SettingsChanged) EventName() string            { return "settings.changed" }
func (SessionDeleted) EventName() string             { return "session.deleted" }
func (SessionStarted) EventName() string             { return "session.started" }
func (SessionEnded) EventName() string               { return "session.ended" }
func (InterviewOpened) EventName() string            { return "interview.opened" }
func (InterviewClosed) EventName() string            { return "interview.closed" }
func (SessionTurnEnded) EventName() string           { return "session.turn_ended" }
func (SessionCommitCheckChanged) EventName() string  { return "session.commit_check_changed" }
func (TicketStatusChanged) EventName() string        { return "ticket.status_changed" }
func (TicketDeleted) EventName() string              { return "ticket.deleted" }
func (TicketResourceAdded) EventName() string        { return "ticket.resource_added" }
func (ReviewRequestChanged) EventName() string       { return "review_request.changed" }
func (SessionStatusChanged) EventName() string       { return "session.status_changed" }
func (SessionWorktreeProvisioned) EventName() string { return "session.worktree_provisioned" }
func (BranchFindingFound) EventName() string         { return "branch.finding_found" }
func (BranchRequestChanged) EventName() string       { return "branch_request.changed" }
func (TicketCommentAdded) EventName() string         { return "ticket.comment_added" }
func (DocumentChanged) EventName() string            { return "document.changed" }
func (ArtifactChanged) EventName() string            { return "artifact.changed" }

func (TokenSaverActivationChanged) EventName() string { return "token_saver.activation_changed" }
func (TokenSaverInstallCompleted) EventName() string  { return "token_saver.install_completed" }

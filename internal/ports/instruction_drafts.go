package ports

import "context"

// InstructionDraftFile is one file of a task's instruction draft: a skill file
// a session wrote for a repository, as the shell shows it (ADR 0092).
type InstructionDraftFile struct {
	// Repository is the repository's name in the project; Skill the skill
	// folder; File the path inside it.
	Repository, Skill, File string
	Content                 []byte
	// Writable is false while an open proposal holds the file's skill.
	Writable bool
}

// InstructionDrafts is what the virtual shell needs of the instruction draft
// of a task: its files for the shell, a check of a write before it lands,
// and the storing of what a session wrote. rel is a path below
// task/instruction-drafts/ (<repository>/<skill>/<file>).
type InstructionDrafts interface {
	ListInstructionDrafts(ctx context.Context, projectID, taskID string) ([]InstructionDraftFile, error)
	// CheckInstructionDraft refuses with INSTRUCTION_INVALID a path that could
	// never become a skill file, and with PROPOSAL_SUBMITTED a skill that
	// waits on the person.
	CheckInstructionDraft(ctx context.Context, projectID, taskID, rel string) error
	// PutInstructionDraft stores a file, refusing INSTRUCTION_INVALID over
	// the size a file may have.
	PutInstructionDraft(ctx context.Context, projectID, taskID, rel string, content []byte) error
	RemoveInstructionDraft(ctx context.Context, projectID, taskID, rel string) error
}

// InstructionDraftRow is one stored file of a task's instruction draft.
type InstructionDraftRow struct {
	RepositoryID string
	// Path is the repository path the file becomes: .claude/skills/<skill>/<file>.
	Path    string
	Content []byte
}

// InstructionDraftStore keeps the files of tasks' instruction drafts (driven).
type InstructionDraftStore interface {
	ListInstructionDrafts(ctx context.Context, taskID string) ([]InstructionDraftRow, error)
	PutInstructionDraft(ctx context.Context, taskID string, row InstructionDraftRow) error
	DeleteInstructionDraft(ctx context.Context, taskID, repositoryID, path string) error
}

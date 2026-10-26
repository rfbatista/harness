package domain

import "strings"

// Codes of a session branch name that cannot be used (ADR 0036).
const (
	// BranchNameInvalid: the name breaks git's branch-name rules.
	BranchNameInvalid = "BRANCH_NAME_INVALID"
	// BranchExists: refs/heads/<name> exists. A branch is never reused.
	BranchExists = "BRANCH_EXISTS"
)

// BranchNameCheck is the answer to "can a session's branch be named this?".
// It is advisory: the start checks again.
type BranchNameCheck struct {
	// Name is the name checked, trimmed; empty when none was given.
	Name string `json:"name"`
	// Generated is the name a start would make; the form prefills it.
	Generated string `json:"generated"`
	// Valid: a valid git branch name. Available: valid and not yet a branch.
	Valid     bool `json:"valid"`
	Available bool `json:"available"`
	// Code is "", BranchNameInvalid or BranchExists.
	Code   string `json:"code"`
	Reason string `json:"reason"`
	// Suggestion is the first free variant (name-2, name-3, …) of a name that
	// exists; empty otherwise.
	Suggestion string `json:"suggestion"`
}

// GeneratedBranchName is the branch a session on a task gets when its person
// names none, before the session's id suffix. It carries no random part.
func GeneratedBranchName(task string) string {
	slug := Slug(task)
	if slug == "" {
		slug = "session"
	}
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	return "agent/" + slug
}

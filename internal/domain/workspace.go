package domain

import "time"

// Workspace is an isolated git worktree of a repository, materialized on disk
// and tracked in the database. Name is a slug unique within the repository;
// Path is computed by the service, never supplied by callers.
type Workspace struct {
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
	Branch       string `json:"branch"`
	Path         string `json:"path"`
	// BaseRef is the ref the branch was cut from; "" for workspaces made
	// before it was recorded, or cut from the repository's HEAD.
	BaseRef   string    `json:"base_ref,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

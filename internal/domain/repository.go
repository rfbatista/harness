package domain

import (
	"path"
	"strings"
)

type Repository struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	URL          string   `json:"url"`
	RootDir      string   `json:"root_dir"`
	IgnoredPaths []string `json:"ignored_paths,omitempty"`
}

// DisplayName is the repository's name, else its checkout's folder name,
// else its id.
func (r *Repository) DisplayName() string {
	switch {
	case r.Name != "":
		return r.Name
	case r.RootDir != "":
		return path.Base(strings.TrimRight(r.RootDir, "/"))
	default:
		return r.ID
	}
}

// GitBranch is a ref a worktree can be based on. Remote marks refs under
// refs/remotes (origin/main); IsHead marks the repository's checked-out branch,
// which the UI offers as the default base.
type GitBranch struct {
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
	IsHead bool   `json:"is_head"`
}

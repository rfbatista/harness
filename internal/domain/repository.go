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
	// UsesDesignSystem opts the repository into the project design system;
	// DesignSystemPath is where its export goes, relative to the checkout.
	UsesDesignSystem bool   `json:"uses_design_system"`
	DesignSystemPath string `json:"design_system_path"`
}

// DefaultDesignSystemPath is where a repository's design system export goes
// until the person picks another folder.
const DefaultDesignSystemPath = ".harness/design-system/"

// CleanDesignSystemPath checks a repository's design system path: relative,
// ending in "/" and staying inside the repository. An empty path is the
// default. INVALID_INPUT otherwise.
func CleanDesignSystemPath(p string) (string, error) {
	if p == "" {
		return DefaultDesignSystemPath, nil
	}
	bad := func(why string) (string, error) {
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "design system path " + why}
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return bad("must be relative to the repository")
	}
	if !strings.HasSuffix(p, "/") {
		return bad("must end in /")
	}
	for _, seg := range strings.Split(strings.TrimSuffix(p, "/"), "/") {
		if seg == ".." {
			return bad("must stay inside the repository")
		}
		if seg == "" || seg == "." {
			return bad("must not have empty or . segments")
		}
	}
	return p, nil
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

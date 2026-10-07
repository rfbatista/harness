// Package domain defines the core data structures and types used across the application, such as Project, Repository, TreeNode, etc. It serves as the foundation for representing the project's structure and related entities.
package domain

import "strings"

// Project defines the directory root that everything (tree, matching paths, zones) is based on.
// All paths and operations are relative to the project's root.
// IgnoredPaths are paths (files or directories) to hide from the tree view; children of ignored dirs are hidden too.
type Project struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	RootDir      string       `json:"root_dir"`
	IgnoredPaths []string     `json:"ignored_paths,omitempty"`
	Repositories []Repository `json:"repositories,omitempty"`
}

// CleanProjectName trims a project name; an empty one is INVALID_INPUT.
func CleanProjectName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "project name is required"}
	}
	return name, nil
}

// SameProjectName reports whether two project names are the same name:
// project names are unique regardless of case and surrounding spaces.
func SameProjectName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// ProjectSessionRef names a live session of a project: what a person needs
// to find and stop it.
type ProjectSessionRef struct {
	ID       string `json:"id"`
	TicketID string `json:"ticket_id"`
	// Agent is the agent's name; its id when the agent is gone; "" when the
	// session has none.
	Agent string `json:"agent"`
}

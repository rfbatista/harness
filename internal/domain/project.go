// Package domain defines the core data structures and types used across the application, such as Project, Repository, TreeNode, etc. It serves as the foundation for representing the project's structure and related entities.
package domain

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

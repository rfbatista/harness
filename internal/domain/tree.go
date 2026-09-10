package domain

// TreeNode is a node in the source tree (path, name, is_dir, children).
// Used when listing project structure for the designer.
type TreeNode struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	IsDir    bool        `json:"is_dir"`
	Children []*TreeNode `json:"children"`
}

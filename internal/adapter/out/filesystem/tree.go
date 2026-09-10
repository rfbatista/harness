package filesystem

import (
	"os"
	"path/filepath"

	"operators-mcp/internal/application/ports"
	"operators-mcp/internal/domain"
)

// Ensure lister implements ports.TreeLister at compile time.
var _ ports.TreeLister = (*Lister)(nil)

// Lister implements TreeLister using the OS filesystem.
type Lister struct{}

// NewLister returns a new filesystem tree lister.
func NewLister() *Lister {
	return &Lister{}
}

// ListTree builds a tree from root. Root empty means cwd. Returns error if root unreadable.
func (l *Lister) ListTree(root string) (*domain.TreeNode, error) {
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
		}
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
	}
	if !info.IsDir() {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: "root is not a directory"}
	}
	return listTreeAt(root, root, "")
}

func listTreeAt(fullRoot, current, relPath string) (*domain.TreeNode, error) {
	entries, err := os.ReadDir(current)
	if err != nil {
		return nil, &domain.StructuredError{Code: "ROOT_UNREADABLE", Message: err.Error()}
	}
	name := filepath.Base(current)
	if relPath == "" {
		name = "."
	}
	node := &domain.TreeNode{
		Path:     filepath.ToSlash(relPath),
		Name:     name,
		IsDir:    true,
		Children: nil,
	}
	for _, e := range entries {
		childRel := filepath.Join(relPath, e.Name())
		childRel = filepath.ToSlash(childRel)
		childFull := filepath.Join(current, e.Name())
		if e.IsDir() {
			child, err := listTreeAt(fullRoot, childFull, childRel)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, child)
		} else {
			node.Children = append(node.Children, &domain.TreeNode{
				Path:     childRel,
				Name:     e.Name(),
				IsDir:    false,
				Children: nil,
			})
		}
	}
	return node, nil
}

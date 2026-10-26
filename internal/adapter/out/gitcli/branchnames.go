package gitcli

import (
	"context"
	"strings"

	"operators-mcp/internal/ports"
)

var _ ports.GitBranchNames = (*BranchNames)(nil)

// BranchNames reads branch names, each run bounded by the read deadline.
type BranchNames struct{ d config }

// NewBranchNames returns the adapter.
func NewBranchNames(opts ...Option) *BranchNames { return &BranchNames{d: newConfig(opts)} }

// RefFormatProblem asks git whether name is a branch name. git says no with
// no reason, so the reason is a sentence about the rules; the common causes
// are named when they can be seen.
func (b BranchNames) RefFormatProblem(ctx context.Context, dir, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "a branch name cannot be empty", nil
	}
	ok, err := b.d.gitOK(ctx, dir, "check-ref-format", "--branch", name)
	if err != nil {
		return "", err
	}
	if ok {
		return "", nil
	}
	return refFormatReason(name), nil
}

func refFormatReason(name string) string {
	switch {
	case strings.HasPrefix(name, "-"):
		return "a branch name cannot start with '-'"
	case strings.ContainsAny(name, " \t\n"):
		return "a branch name cannot contain spaces"
	case strings.Contains(name, ".."):
		return "a branch name cannot contain '..'"
	case strings.HasSuffix(name, ".lock"):
		return "a branch name cannot end with '.lock'"
	case strings.HasSuffix(name, "/") || strings.HasPrefix(name, "/") || strings.Contains(name, "//"):
		return "a branch name cannot start or end with '/' or hold an empty part"
	case strings.HasSuffix(name, "."):
		return "a branch name cannot end with '.'"
	case strings.Contains(name, "@{"):
		return "a branch name cannot contain '@{'"
	}
	return "this is not a valid git branch name (see git check-ref-format)"
}

// LocalBranches lists refs/heads with one for-each-ref.
func (b BranchNames) LocalBranches(ctx context.Context, dir string) ([]string, error) {
	out, err := b.d.gitOutput(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

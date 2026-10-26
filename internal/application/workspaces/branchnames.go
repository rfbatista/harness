package workspaces

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchNameChecker = (*BranchNames)(nil)

// BranchNames checks the name a person typed for a session's branch (ADR
// 0036). It reads git only: a check creates nothing and never fetches.
type BranchNames struct {
	repos   ports.RepositoryRepository
	tickets ports.TicketRepository // nil: the generated name carries no task slug
	git     ports.GitBranchNames
}

// NewBranchNames returns the checker.
func NewBranchNames(repos ports.RepositoryRepository, tickets ports.TicketRepository, git ports.GitBranchNames) *BranchNames {
	return &BranchNames{repos: repos, tickets: tickets, git: git}
}

// CheckBranchName costs two git runs however many branches there are: the
// name against git's rules, and the list of local branches, from which both
// the existence and the free variant are read.
func (s *BranchNames) CheckBranchName(ctx context.Context, repositoryID, name, ticketID string) (*domain.BranchNameCheck, error) {
	repo := s.repos.Get(ctx, repositoryID)
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository not found"}
	}
	if strings.TrimSpace(repo.RootDir) == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir"}
	}
	root := domain.ExpandUserPath(repo.RootDir)
	name = strings.TrimSpace(name)
	out := &domain.BranchNameCheck{Name: name, Generated: s.generated(ctx, ticketID)}
	if name == "" {
		return out, nil
	}
	problem, err := s.git.RefFormatProblem(ctx, root, name)
	if err != nil {
		return nil, err
	}
	if problem != "" {
		out.Code, out.Reason = domain.BranchNameInvalid, problem
		return out, nil
	}
	out.Valid = true
	branches, err := s.git.LocalBranches(ctx, root)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(branches, name) {
		out.Available = true
		return out, nil
	}
	out.Code, out.Reason = domain.BranchExists, "the branch "+name+" already exists"
	out.Suggestion = freeVariant(name, branches)
	return out, nil
}

// generated is the name a start would make, with a random suffix standing in
// for the session id's.
func (s *BranchNames) generated(ctx context.Context, ticketID string) string {
	title := "session"
	if s.tickets != nil && ticketID != "" {
		if tk := s.tickets.Get(ctx, ticketID); tk != nil {
			title = tk.Title
		}
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return domain.GeneratedBranchName(title) + "-" + hex.EncodeToString(b[:])
}

// freeVariant is the first of name-2, name-3, … that is not a branch.
func freeVariant(name string, branches []string) string {
	for n := 2; ; n++ {
		if v := name + "-" + strconv.Itoa(n); !slices.Contains(branches, v) {
			return v
		}
	}
}

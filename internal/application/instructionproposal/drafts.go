// Package instructionproposal is the instruction draft of a task and the
// instruction-file proposals made from it (ADR 0092): sessions draft skills
// in their virtual shell and submit them, the person decides the whole
// proposal, and an approval saves new instruction-file versions.
package instructionproposal

import (
	"context"
	"fmt"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionDrafts = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Drafts       ports.InstructionDraftStore
	Repositories ports.RepositoryLister
}

// Service keeps the instruction drafts and the proposals.
type Service struct {
	drafts ports.InstructionDraftStore
	repos  ports.RepositoryLister
}

// NewService returns a new Service.
func NewService(d Deps) *Service { return &Service{drafts: d.Drafts, repos: d.Repositories} }

func invalid(problems ...string) error {
	return &domain.StructuredError{Code: "INSTRUCTION_INVALID", Message: strings.Join(problems, "; ")}
}

// repositoryByName is the project's repository the draft folder names.
func (s *Service) repositoryByName(ctx context.Context, projectID, name string) (*domain.Repository, error) {
	repos, err := s.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, r := range repos {
		if r.DisplayName() == name {
			return r, nil
		}
	}
	return nil, nil
}

// resolve checks rel, a path below task/instruction-drafts/, and answers the
// repository and the path of the file it becomes.
func (s *Service) resolve(ctx context.Context, projectID, rel string) (*domain.Repository, domain.InstructionDraftPath, error) {
	p, problems := domain.ParseInstructionDraftPath(rel)
	if len(problems) > 0 {
		return nil, p, invalid(problems...)
	}
	repo, err := s.repositoryByName(ctx, projectID, p.Repository)
	if err != nil {
		return nil, p, err
	}
	if repo == nil {
		return nil, p, invalid(fmt.Sprintf("%q is not a repository of the project: use a name from list_project_repositories", p.Repository))
	}
	return repo, p, nil
}

// CheckInstructionDraft implements ports.InstructionDrafts.
func (s *Service) CheckInstructionDraft(ctx context.Context, projectID, taskID, rel string) error {
	_, _, err := s.resolve(ctx, projectID, rel)
	return err
}

// PutInstructionDraft implements ports.InstructionDrafts.
func (s *Service) PutInstructionDraft(ctx context.Context, projectID, taskID, rel string, content []byte) error {
	repo, p, err := s.resolve(ctx, projectID, rel)
	if err != nil {
		return err
	}
	if len(content) > domain.MaxInstructionFileSize {
		return invalid(fmt.Sprintf("%s: a file is at most %d KB, this one is %d bytes", p.File, domain.MaxInstructionFileSize/1024, len(content)))
	}
	return s.drafts.PutInstructionDraft(ctx, taskID, ports.InstructionDraftRow{RepositoryID: repo.ID, Path: p.InstructionPath(), Content: content})
}

// RemoveInstructionDraft implements ports.InstructionDrafts.
func (s *Service) RemoveInstructionDraft(ctx context.Context, projectID, taskID, rel string) error {
	repo, p, err := s.resolve(ctx, projectID, rel)
	if err != nil {
		return err
	}
	return s.drafts.DeleteInstructionDraft(ctx, taskID, repo.ID, p.InstructionPath())
}

// ListInstructionDrafts implements ports.InstructionDrafts. A file of a
// repository the project no longer has is left out.
func (s *Service) ListInstructionDrafts(ctx context.Context, projectID, taskID string) ([]ports.InstructionDraftFile, error) {
	rows, err := s.drafts.ListInstructionDrafts(ctx, taskID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	repos, err := s.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, r := range repos {
		names[r.ID] = r.DisplayName()
	}
	out := make([]ports.InstructionDraftFile, 0, len(rows))
	for _, row := range rows {
		name, ok := names[row.RepositoryID]
		skill, file, cut := strings.Cut(strings.TrimPrefix(row.Path, ".claude/skills/"), "/")
		if !ok || !cut {
			continue
		}
		out = append(out, ports.InstructionDraftFile{Repository: name, Skill: skill, File: file, Content: row.Content, Writable: true})
	}
	return out, nil
}

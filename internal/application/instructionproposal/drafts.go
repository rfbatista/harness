// Package instructionproposal is the instruction draft of a task and the
// instruction-file proposals made from it (ADR 0092): sessions draft skills
// in their virtual shell and submit them, the person decides the whole
// proposal, and an approval saves new instruction-file versions.
package instructionproposal

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionDrafts = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Drafts       ports.InstructionDraftStore
	Proposals    ports.InstructionProposalStore
	Repositories interface {
		ports.RepositoryLister
		ports.RepositoryReader
	}
	// Files reads the repository's saved instruction files, for the base of a
	// proposal.
	Files InstructionFileReader
}

// InstructionFileReader is the saved instruction files of a repository.
type InstructionFileReader interface {
	ListInstructionFiles(ctx context.Context, repositoryID string) ([]domain.InstructionFile, error)
	GetInstructionFile(ctx context.Context, repositoryID, path string) (*domain.InstructionFile, error)
}

// Service keeps the instruction drafts and the proposals.
type Service struct {
	drafts    ports.InstructionDraftStore
	proposals ports.InstructionProposalStore
	repos     interface {
		ports.RepositoryLister
		ports.RepositoryReader
	}
	tasks    ports.TicketReader
	agents   ports.AgentLister
	files    InstructionFileReader
	sessions ports.SessionRepository

	notifier ports.Notifier
	feed     ports.ProjectChangeSink
	mu       sync.Mutex // submits and decisions exclude each other
}

// NewService returns a new Service.
func NewService(d Deps) *Service {
	return &Service{drafts: d.Drafts, proposals: d.Proposals, repos: d.Repositories, files: d.Files}
}

// UseSessions sets where the session a proposal comes from is read.
func (s *Service) UseSessions(r ports.SessionRepository) { s.sessions = r }

// UseNotifier sets where a submitted proposal is announced to the person's inbox.
func (s *Service) UseNotifier(n ports.Notifier) { s.notifier = n }

// UseFeed sets where a proposal reaching a state is announced.
func (s *Service) UseFeed(f ports.ProjectChangeSink) { s.feed = f }

// lockedSkills are the skills of the task's open proposals, by "<repository
// id>/<skill>", each with its proposal's id.
func (s *Service) lockedSkills(ctx context.Context, taskID string) (map[string]string, error) {
	locked := map[string]string{}
	if s.proposals == nil {
		return locked, nil
	}
	list, err := s.proposals.ListInstructionProposalsByTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if p.State != domain.InstructionProposalOpen {
			continue
		}
		for _, skill := range p.Skills {
			locked[p.RepositoryID+"/"+skill] = p.ID
		}
	}
	return locked, nil
}

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
	repo, p, err := s.resolve(ctx, projectID, rel)
	if err != nil {
		return err
	}
	return s.refuseSubmitted(ctx, taskID, repo.ID, p.Skill)
}

// refuseSubmitted is PROPOSAL_SUBMITTED while an open proposal holds the skill.
func (s *Service) refuseSubmitted(ctx context.Context, taskID, repositoryID, skill string) error {
	locked, err := s.lockedSkills(ctx, taskID)
	if err != nil {
		return err
	}
	if id, ok := locked[repositoryID+"/"+skill]; ok {
		return &domain.StructuredError{Code: "PROPOSAL_SUBMITTED", Message: "this skill waits on the person in proposal " + id + "; it returns to you if they send it back"}
	}
	return nil
}

// PutInstructionDraft implements ports.InstructionDrafts.
func (s *Service) PutInstructionDraft(ctx context.Context, projectID, taskID, rel string, content []byte) error {
	repo, p, err := s.resolve(ctx, projectID, rel)
	if err != nil {
		return err
	}
	if err := s.refuseSubmitted(ctx, taskID, repo.ID, p.Skill); err != nil {
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
	if err := s.refuseSubmitted(ctx, taskID, repo.ID, p.Skill); err != nil {
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
	locked, err := s.lockedSkills(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]ports.InstructionDraftFile, 0, len(rows))
	for _, row := range rows {
		name, ok := names[row.RepositoryID]
		skill, file, cut := strings.Cut(strings.TrimPrefix(row.Path, ".claude/skills/"), "/")
		if !ok || !cut {
			continue
		}
		out = append(out, ports.InstructionDraftFile{Repository: name, Skill: skill, File: file, Content: row.Content, Writable: locked[row.RepositoryID+"/"+skill] == ""})
	}
	return out, nil
}

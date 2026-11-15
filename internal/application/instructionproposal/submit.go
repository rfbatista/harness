package instructionproposal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionProposalSubmitter = (*Service)(nil)

func invalidInput(msg string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg}
}

func newProposalID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "ip_" + hex.EncodeToString(b[:])
}

// SubmitInstructionProposal implements ports.InstructionProposalSubmitter:
// the named draft skills of one repository become one open proposal.
func (s *Service) SubmitInstructionProposal(ctx context.Context, sessionID, repository string, skills []string, summary string) (*ports.InstructionSubmission, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, invalidInput("summary is required: say in a line what the skills are for")
	}
	var wanted []string
	for _, name := range skills {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(wanted, name) {
			wanted = append(wanted, name)
		}
	}
	if len(wanted) == 0 {
		return nil, invalidInput("skills is required: name the draft skills to submit")
	}
	sess := s.sessions.Get(sessionID)
	if sess == nil || sess.TicketID == "" {
		return nil, &domain.StructuredError{Code: "SESSION_HAS_NO_TASK", Message: "this session is not working on a task"}
	}
	repo, err := s.repositoryByName(ctx, sess.ProjectID, strings.TrimSpace(repository))
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: fmt.Sprintf("%q is not a repository of the project: use a name from list_project_repositories", repository)}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	open, err := s.openSkills(ctx, repo.ID)
	if err != nil {
		return nil, err
	}
	for _, skill := range wanted {
		if id, ok := open[skill]; ok {
			return nil, &domain.StructuredError{Code: "PROPOSAL_SUBMITTED", Message: fmt.Sprintf("the skill %s is already in the open proposal %s", skill, id)}
		}
	}
	rows, err := s.drafts.ListInstructionDrafts(ctx, sess.TicketID)
	if err != nil {
		return nil, err
	}
	files, err := s.proposalFiles(ctx, repo.ID, wanted, rows)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, &domain.StructuredError{Code: "PROPOSAL_EMPTY", Message: "the draft equals the repository's current instruction files: change a draft file first"}
	}

	p := &domain.InstructionProposal{
		ID: newProposalID(), RepositoryID: repo.ID, ProjectID: sess.ProjectID, TaskID: sess.TicketID, SessionID: sessionID,
		Summary: summary, Skills: wanted, State: domain.InstructionProposalOpen, Files: files, CreatedAt: time.Now().UTC(),
	}
	if err := s.proposals.CreateInstructionProposal(ctx, p); err != nil {
		return nil, err
	}
	s.announce(ctx, p)
	s.notifySubmitted(ctx, repo, sess, p)

	out := &ports.InstructionSubmission{ProposalID: p.ID, Files: len(files)}
	for _, f := range files {
		switch f.Op {
		case domain.InstructionOpCreate:
			out.Created++
		case domain.InstructionOpChange:
			out.Changed++
		case domain.InstructionOpRemove:
			out.Removed++
		}
	}
	return out, nil
}

// openSkills are the skills of the repository's open proposals, with the
// proposal that holds each.
func (s *Service) openSkills(ctx context.Context, repositoryID string) (map[string]string, error) {
	list, err := s.proposals.ListInstructionProposalsByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	open := map[string]string{}
	for _, p := range list {
		if p.State == domain.InstructionProposalOpen {
			for _, skill := range p.Skills {
				open[skill] = p.ID
			}
		}
	}
	return open, nil
}

// openCount is how many proposals of the repository wait on the person.
func (s *Service) openCount(ctx context.Context, repositoryID string) int {
	open, err := s.proposals.ListInstructionProposalsByRepository(ctx, repositoryID)
	if err != nil {
		slog.Warn("count open instruction proposals", "repository", repositoryID, "err", err)
		return 0
	}
	n := 0
	for _, p := range open {
		if p.State == domain.InstructionProposalOpen {
			n++
		}
	}
	return n
}

// proposalFiles is what the draft skills would change in the repository's
// instruction files: a file the repository has not is a create, one that
// differs a change, and a saved file of a skill that has no draft counterpart a
// remove. Identical files are left out. A skill that has no draft, or a draft
// that could not be saved as it is, refuses the whole submit.
func (s *Service) proposalFiles(ctx context.Context, repositoryID string, skills []string, rows []ports.InstructionDraftRow) ([]domain.InstructionProposalFile, error) {
	saved, err := s.files.ListInstructionFiles(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	var out []domain.InstructionProposalFile
	var problems []string
	for _, skill := range skills {
		prefix := ".claude/skills/" + skill + "/"
		drafted := map[string]string{}
		for _, row := range rows {
			if row.RepositoryID == repositoryID && strings.HasPrefix(row.Path, prefix) {
				drafted[row.Path] = string(row.Content)
			}
		}
		if len(drafted) == 0 {
			return nil, invalidInput(fmt.Sprintf("the skill %s has no draft: write task/instruction-drafts/<repository>/%s/SKILL.md first", skill, skill))
		}
		problems = append(problems, skillProblems(skill, prefix, drafted)...)
		paths := make([]string, 0, len(drafted))
		for path := range drafted {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		for _, path := range paths {
			cur, err := s.currentFile(ctx, repositoryID, saved, path)
			if err != nil {
				return nil, err
			}
			switch {
			case cur == nil:
				out = append(out, domain.InstructionProposalFile{Path: path, Op: domain.InstructionOpCreate, Content: drafted[path]})
			case cur.Content != drafted[path]:
				out = append(out, domain.InstructionProposalFile{Path: path, Op: domain.InstructionOpChange, BaseVersion: cur.Version, BaseContent: cur.Content, Content: drafted[path]})
			}
		}
		for _, f := range saved {
			if _, kept := drafted[f.Path]; strings.HasPrefix(f.Path, prefix) && !kept {
				cur, err := s.files.GetInstructionFile(ctx, repositoryID, f.Path)
				if err != nil {
					return nil, err
				}
				out = append(out, domain.InstructionProposalFile{Path: f.Path, Op: domain.InstructionOpRemove, BaseVersion: cur.Version, BaseContent: cur.Content})
			}
		}
	}
	if len(problems) > 0 {
		return nil, invalid(problems...)
	}
	return out, nil
}

// currentFile is the saved file at path with its content, nil when none.
func (s *Service) currentFile(ctx context.Context, repositoryID string, saved []domain.InstructionFile, path string) (*domain.InstructionFile, error) {
	if !slices.ContainsFunc(saved, func(f domain.InstructionFile) bool { return f.Path == path }) {
		return nil, nil
	}
	return s.files.GetInstructionFile(ctx, repositoryID, path)
}

// skillProblems lists what stops the drafted files of a skill being saved: the
// instruction-file validation of each, a SKILL.md, and its frontmatter name and
// description.
func skillProblems(skill, prefix string, drafted map[string]string) []string {
	var problems []string
	for path, content := range drafted {
		for _, e := range domain.ValidateInstructionFile(path, "skill", content) {
			problems = append(problems, path+": "+e.Message)
		}
	}
	skillMD, ok := drafted[prefix+"SKILL.md"]
	if !ok {
		return append(problems, fmt.Sprintf("the skill %s has no SKILL.md", skill))
	}
	front := frontmatter(skillMD)
	for _, key := range []string{"name", "description"} {
		if front[key] == "" {
			problems = append(problems, fmt.Sprintf("%sSKILL.md has no frontmatter %s", prefix, key))
		}
	}
	slices.Sort(problems)
	return problems
}

// frontmatter reads the top-level scalar keys of the YAML block a file opens
// with, between two "---" lines.
func frontmatter(content string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return out
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			return out
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return map[string]string{} // never closed: no frontmatter
}

// announce puts the proposal's state and its repository's open count on the feed.
func (s *Service) announce(ctx context.Context, p *domain.InstructionProposal) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{InstructionProposalChanged: &ports.InstructionProposalChange{
		RepositoryID: p.RepositoryID, ProposalID: p.ID, State: p.State, OpenCount: s.openCount(ctx, p.RepositoryID), ProjectID: p.ProjectID,
	}})
}

// notifySubmitted posts the inbox notification of a proposal that opened: level
// action, linking the repository's Instructions & rules workspace with the
// proposal open. A failure is logged: the proposal is open.
func (s *Service) notifySubmitted(ctx context.Context, repo *domain.Repository, sess *domain.Session, p *domain.InstructionProposal) {
	if s.notifier == nil {
		return
	}
	_, err := s.notifier.Notify(ctx, ports.NotifyRequest{
		SessionID: sess.ID, ProjectID: p.ProjectID,
		Title: "Instruction files proposed: " + strings.Join(p.Skills, ", ") + " in " + repo.DisplayName(),
		Body:  p.Summary,
		Level: domain.NotificationAction,
		Link:  "/projects/" + url.PathEscape(p.ProjectID) + "/repositories/" + url.PathEscape(repo.ID) + "/architecture/instructions?proposal=" + url.QueryEscape(p.ID),
	})
	if err != nil {
		slog.Warn("notify a submitted instruction proposal", "proposal", p.ID, "err", err)
	}
}

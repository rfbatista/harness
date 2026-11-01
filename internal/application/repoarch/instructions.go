package repoarch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionFiles = (*Service)(nil)

// tooBigAsInvalid turns the store's refusal of a save over the total limit
// into the INSTRUCTION_INVALID the contract names; any other error is as it was.
func tooBigAsInvalid(err error) error {
	if !errors.Is(err, ports.ErrInstructionFilesTooBig) {
		return err
	}
	return invalidInstruction([]domain.ArchFieldError{{Path: "/content", Code: domain.ArchInvalidValue,
		Message: fmt.Sprintf("all instruction files of a repository together are at most %.2f MiB (4 MiB less 256 KB kept for ARCHITECTURE.md)", float64(domain.MaxInstructionFilesSize)/(1<<20))}})
}

// invalidInstruction is INSTRUCTION_INVALID with a field error per problem.
func invalidInstruction(problems []domain.ArchFieldError) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "INSTRUCTION_INVALID", Message: "the instruction file is invalid: " + problems[0].Message},
		Details:         map[string]any{"errors": problems},
	}
}

// announceInstruction puts a file change on the project feed.
func (s *Service) announceInstruction(projectID, repositoryID, path string, version int) {
	if s.feed == nil {
		return
	}
	s.feed.AnnounceChange(ports.ProjectChange{InstructionFileChanged: &ports.InstructionFileChange{
		RepositoryID: repositoryID, Path: path, Version: version, ProjectID: projectID,
	}})
}

// ListInstructionFiles implements ports.InstructionFiles. The content is left
// out: a list is metadata only.
func (s *Service) ListInstructionFiles(ctx context.Context, repositoryID string) ([]domain.InstructionFile, error) {
	if _, err := s.repos.GetRepository(ctx, repositoryID); err != nil {
		return nil, err
	}
	files, err := s.store.ListInstructionFiles(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.InstructionFile, len(files))
	for i, f := range files {
		out[i] = f
		out[i].Content = ""
	}
	return out, nil
}

// GetInstructionFile implements ports.InstructionFiles.
func (s *Service) GetInstructionFile(ctx context.Context, repositoryID, path string) (*domain.InstructionFile, error) {
	if _, err := s.repos.GetRepository(ctx, repositoryID); err != nil {
		return nil, err
	}
	return s.store.GetInstructionFile(ctx, repositoryID, path)
}

// SaveInstructionFile implements ports.InstructionFiles.
func (s *Service) SaveInstructionFile(ctx context.Context, repositoryID, path, kind string, baseVersion int, content string) (*domain.InstructionFile, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	kind = domain.CurrentInstructionKind(kind)
	if problems := domain.ValidateInstructionFile(path, kind, content); len(problems) > 0 {
		return nil, invalidInstruction(problems)
	}
	f, err := s.store.SaveInstructionFile(ctx, domain.InstructionFile{
		RepositoryID: repositoryID, ProjectID: repo.ProjectID, Path: path, Kind: kind, Content: content,
	}, baseVersion, domain.MaxInstructionFilesSize)
	if err != nil {
		return nil, tooBigAsInvalid(err)
	}
	s.announceInstruction(repo.ProjectID, repositoryID, path, f.Version)
	return f, nil
}

// RenameInstructionFile implements ports.InstructionFiles. The file's record
// entry moves with it, in the same transaction.
func (s *Service) RenameInstructionFile(ctx context.Context, repositoryID, path, newPath string, baseVersion int) (*domain.InstructionFile, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	old, err := s.store.GetInstructionFile(ctx, repositoryID, path)
	if err != nil {
		return nil, err
	}
	if problems := domain.ValidateInstructionFile(newPath, old.Kind, ""); len(problems) > 0 {
		return nil, invalidInstruction(problems)
	}
	f, recordVersion, err := s.store.RenameInstructionFile(ctx, repositoryID, path, newPath, baseVersion)
	if err != nil {
		return nil, err
	}
	s.announceRecord(repo, recordVersion)
	s.announceInstruction(repo.ProjectID, repositoryID, path, 0)
	s.announceInstruction(repo.ProjectID, repositoryID, newPath, f.Version)
	return f, nil
}

// DeleteInstructionFile implements ports.InstructionFiles. The file's record
// entry goes with it, in the same transaction.
func (s *Service) DeleteInstructionFile(ctx context.Context, repositoryID, path string, baseVersion int) error {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	recordVersion, err := s.store.DeleteInstructionFile(ctx, repositoryID, path, baseVersion)
	if err != nil {
		return err
	}
	s.announceRecord(repo, recordVersion)
	s.announceInstruction(repo.ProjectID, repositoryID, path, 0)
	return nil
}

// announceRecord tells the record's followers it has a new version, after a file
// change moved or removed one of its entries; version 0 says it did not change.
func (s *Service) announceRecord(repo *domain.Repository, version int) {
	if version == 0 {
		return
	}
	s.forget(repo.ID)
	s.announce(repo.ProjectID, repo.ID, version)
}

// ListRepositoryCopies implements ports.InstructionFiles.
func (s *Service) ListRepositoryCopies(ctx context.Context, repositoryID string) ([]domain.RepositoryCopy, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	branch, files, err := s.defaultBranchFiles(ctx, repo)
	if err != nil {
		// A checkout that cannot be read has no copies to show; the harness's
		// own files still list.
		slog.Warn("list the instruction files of a checkout", "repository", repositoryID, "err", err)
		return []domain.RepositoryCopy{}, nil
	}
	harness, err := s.store.ListInstructionFiles(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	have := map[string]string{}
	for _, f := range harness {
		have[f.Path] = f.Content
	}
	out := []domain.RepositoryCopy{}
	for _, f := range files {
		kind := domain.InstructionKindOf(f)
		if kind == "" {
			continue
		}
		blob, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, f)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		mine, ok := have[f]
		out = append(out, domain.RepositoryCopy{Path: f, Kind: kind, Differs: !ok || mine != string(blob)})
	}
	return out, nil
}

// ImportInstructionFiles implements ports.InstructionFiles.
func (s *Service) ImportInstructionFiles(ctx context.Context, repositoryID string, paths []string) ([]domain.InstructionFile, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if s.git == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the checkout cannot be read"}
	}
	branch, err := s.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil {
		return nil, err
	}
	type source struct {
		path, kind, content string
	}
	var sources []source
	for _, p := range paths {
		kind := domain.InstructionKindOf(p)
		if kind == "" {
			return nil, invalidInstruction([]domain.ArchFieldError{{Path: "/paths", Code: domain.ArchInvalidValue, Message: p + " is not an instruction file"}})
		}
		blob, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, p)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, &domain.StructuredError{Code: "INSTRUCTION_NOT_FOUND", Message: p + " is not on the default branch " + branch}
		}
		if problems := domain.ValidateInstructionFile(p, kind, string(blob)); len(problems) > 0 {
			return nil, invalidInstruction(problems)
		}
		sources = append(sources, source{p, kind, string(blob)})
	}
	// Refuse a batch that would go over the total before anything is saved, so an
	// import is all or nothing.
	stored, err := s.store.ListInstructionFiles(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	total := map[string]int{}
	for _, f := range stored {
		total[f.Path] = len(f.Content)
	}
	for _, src := range sources {
		total[src.path] = len(src.content)
	}
	sum := 0
	for _, n := range total {
		sum += n
	}
	if sum > domain.MaxInstructionFilesSize {
		return nil, tooBigAsInvalid(ports.ErrInstructionFilesTooBig)
	}
	out := []domain.InstructionFile{}
	for _, src := range sources {
		cur, err := s.store.GetInstructionFile(ctx, repositoryID, src.path)
		base := 0
		switch {
		case err == nil:
			if cur.Content == src.content && cur.Kind == src.kind {
				out = append(out, *cur)
				continue
			}
			base = cur.Version
		case isCode(err, "INSTRUCTION_NOT_FOUND"):
			// A new file: it is created at version 0 below.
		default:
			return nil, err
		}
		f, err := s.store.SaveInstructionFile(ctx, domain.InstructionFile{
			RepositoryID: repositoryID, ProjectID: repo.ProjectID, Path: src.path, Kind: src.kind, Content: src.content,
		}, base, domain.MaxInstructionFilesSize)
		if err != nil {
			return nil, tooBigAsInvalid(err)
		}
		s.announceInstruction(repo.ProjectID, repositoryID, src.path, f.Version)
		out = append(out, *f)
	}
	for i := range out {
		out[i].Content = ""
	}
	return out, nil
}

// isCode says err is a StructuredError with code.
func isCode(err error, code string) bool {
	var se *domain.StructuredError
	return errors.As(err, &se) && se.Code == code
}

// removeMessage is the commit message of a removal.
const removeMessage = "chore(architecture): move instruction files into the harness"

// RemoveInstructionFilesFromRepository implements ports.InstructionFiles.
func (s *Service) RemoveInstructionFilesFromRepository(ctx context.Context, repositoryID string, paths []string) (string, error) {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	if s.git == nil {
		return "", &domain.StructuredError{Code: "UNAVAILABLE", Message: "the checkout cannot be written"}
	}
	if len(paths) == 0 {
		return "", &domain.StructuredError{Code: "INVALID_INPUT", Message: "paths is required"}
	}
	defer s.lockCheckout(repo.RootDir)()

	for _, p := range paths {
		if _, err := s.store.GetInstructionFile(ctx, repositoryID, p); err != nil {
			if isCode(err, "INSTRUCTION_NOT_FOUND") {
				return "", &domain.StructuredError{Code: "INSTRUCTION_NOT_IMPORTED", Message: p + " is not in the harness yet; import it first"}
			}
			return "", err
		}
	}
	branch, err := s.cleanDefaultBranch(ctx, repo)
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		if _, found, err := s.git.ReadFile(ctx, repo.RootDir, branch, p); err != nil {
			return "", err
		} else if !found {
			return "", &domain.StructuredError{Code: "INSTRUCTION_NOT_FOUND", Message: p + " is not on the default branch " + branch}
		}
	}
	return s.git.RemoveFiles(ctx, repo.RootDir, paths, removeMessage)
}

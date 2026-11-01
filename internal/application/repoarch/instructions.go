package repoarch

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionFiles = (*Service)(nil)

func invalidInstruction(problems []domain.ArchFieldError) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "INSTRUCTION_INVALID", Message: "the instruction file is invalid: " + problems[0].Message},
		Details:         map[string]any{"errors": problems},
	}
}

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
		return nil, err
	}
	s.announceInstruction(repo.ProjectID, repositoryID, path, f.Version)
	return f, nil
}

// RenameInstructionFile implements ports.InstructionFiles.
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
	f, err := s.store.RenameInstructionFile(ctx, repositoryID, path, newPath, baseVersion)
	if err != nil {
		return nil, err
	}
	if err := s.updateRecordEntries(ctx, repo, func(in []domain.ArchInstruction) []domain.ArchInstruction {
		for i := range in {
			if in[i].Path == path {
				in[i].Path = newPath
			}
		}
		return in
	}); err != nil {
		return nil, err
	}
	s.announceInstruction(repo.ProjectID, repositoryID, path, 0)
	s.announceInstruction(repo.ProjectID, repositoryID, newPath, f.Version)
	return f, nil
}

// DeleteInstructionFile implements ports.InstructionFiles.
func (s *Service) DeleteInstructionFile(ctx context.Context, repositoryID, path string, baseVersion int) error {
	repo, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil {
		return err
	}
	if err := s.store.DeleteInstructionFile(ctx, repositoryID, path, baseVersion); err != nil {
		return err
	}
	if err := s.updateRecordEntries(ctx, repo, func(in []domain.ArchInstruction) []domain.ArchInstruction {
		return slices.DeleteFunc(in, func(e domain.ArchInstruction) bool { return e.Path == path })
	}); err != nil {
		return err
	}
	s.announceInstruction(repo.ProjectID, repositoryID, path, 0)
	return nil
}

// updateRecordEntries applies edit to the record's instruction entries and,
// when it changed any, stores the record as a new version. A repository with
// no record has nothing to keep in step.
func (s *Service) updateRecordEntries(ctx context.Context, repo *domain.Repository, edit func([]domain.ArchInstruction) []domain.ArchInstruction) error {
	stored, err := s.store.Get(ctx, repo.ID)
	if err != nil || stored == nil {
		return err
	}
	rec := stored.Record
	before := slices.Clone(rec.Instructions)
	rec.Instructions = edit(slices.Clone(rec.Instructions))
	if slices.EqualFunc(before, rec.Instructions, func(a, b domain.ArchInstruction) bool {
		return a.Path == b.Path
	}) {
		return nil
	}
	saved, err := s.store.SetRecord(ctx, repo.ID, rec)
	if err != nil || saved == nil {
		return err
	}
	s.forget(repo.ID)
	s.announce(repo.ProjectID, repo.ID, saved.Version)
	return nil
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
		default:
			return nil, err
		}
		f, err := s.store.SaveInstructionFile(ctx, domain.InstructionFile{
			RepositoryID: repositoryID, ProjectID: repo.ProjectID, Path: src.path, Kind: src.kind, Content: src.content,
		}, base, domain.MaxInstructionFilesSize)
		if err != nil {
			return nil, err
		}
		s.announceInstruction(repo.ProjectID, repositoryID, src.path, f.Version)
		out = append(out, *f)
	}
	for i := range out {
		out[i].Content = ""
	}
	return out, nil
}

func isCode(err error, code string) bool {
	var se *domain.StructuredError
	return errors.As(err, &se) && se.Code == code
}

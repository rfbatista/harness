package repoarch

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ArchitectureFileReader = (*Service)(nil)

// ListArchitectureFiles implements ports.ArchitectureFileReader: the YAML of
// each of the project's repositories that has a record, in the repositories'
// order.
func (s *Service) ListArchitectureFiles(ctx context.Context, projectID string) ([]ports.ArchitectureFile, error) {
	repos, err := s.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	byRepo := map[string]*ports.StoredArchitecture{}
	for _, st := range stored {
		byRepo[st.RepositoryID] = st
	}
	var out []ports.ArchitectureFile
	for _, r := range repos {
		st := byRepo[r.ID]
		if st == nil {
			continue
		}
		text, err := ArchitectureYAML(st.Record)
		if err != nil {
			return nil, err
		}
		out = append(out, ports.ArchitectureFile{
			RepositoryID: r.ID, Repository: r.DisplayName(), Version: st.Version, YAML: text, UpdatedAt: st.UpdatedAt,
		})
	}
	return out, nil
}

var _ ports.ArchitectureBriefSource = (*Service)(nil)

// ListArchitectureRefs implements ports.ArchitectureBriefSource. The paths
// are the ones a fresh virtual shell gives the files: the repository's name,
// with -<short id> for a name an earlier repository has.
func (s *Service) ListArchitectureRefs(ctx context.Context, projectID string) ([]ports.ArchitectureRef, error) {
	repos, err := s.repos.ListRepositories(ctx, projectID)
	if err != nil {
		return nil, err
	}
	stored, err := s.store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	has := map[string]bool{}
	for _, st := range stored {
		has[st.RepositoryID] = true
	}
	var out []ports.ArchitectureRef
	taken := map[string]bool{}
	for _, r := range repos {
		if !has[r.ID] {
			continue
		}
		name := domain.Slug(r.DisplayName())
		if name == "" {
			name = "repository"
		}
		if taken[name] {
			name += "-" + shortID(r.ID)
		}
		taken[name] = true
		out = append(out, ports.ArchitectureRef{RepositoryID: r.ID, Repository: r.DisplayName(), Path: "project/architecture/" + name + ".yaml"})
	}
	return out, nil
}

// SavedArchitecture implements ports.ArchitectureBriefSource.
func (s *Service) SavedArchitecture(ctx context.Context, repositoryID string) (*domain.ArchitectureRecord, error) {
	cur, err := s.Current(ctx, repositoryID)
	if err != nil || cur == nil {
		return nil, err
	}
	return cur.Compiled.Record, nil
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

package repoarch

import (
	"context"

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

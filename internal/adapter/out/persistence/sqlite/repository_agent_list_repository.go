package sqlite

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RepositoryAgentListStore = (*RepositoryAgentListRepository)(nil)

// RepositoryAgentListRepository stores each repository's unassigned agents.
type RepositoryAgentListRepository struct{ db *gorm.DB }

// NewRepositoryAgentListRepository returns the store.
func NewRepositoryAgentListRepository(db *gorm.DB) *RepositoryAgentListRepository {
	return &RepositoryAgentListRepository{db: db}
}

// Get returns the repository's list; a repository never written has an empty
// list with version "".
func (r *RepositoryAgentListRepository) Get(ctx context.Context, repositoryID string) (*domain.RepositoryAgentList, error) {
	var m RepositoryAgentListModel
	res := r.db.WithContext(ctx).Limit(1).Find(&m, "repository_id = ?", repositoryID)
	if res.Error != nil {
		return nil, res.Error
	}
	return &domain.RepositoryAgentList{RepositoryID: repositoryID, Agents: append([]string{}, m.Agents...), Version: m.Version}, nil
}

// Put replaces the repository's list.
func (r *RepositoryAgentListRepository) Put(ctx context.Context, l *domain.RepositoryAgentList) error {
	m := RepositoryAgentListModel{RepositoryID: l.RepositoryID, Agents: stringSlice(append([]string{}, l.Agents...)), Version: l.Version}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"agents", "version"}),
	}).Create(&m).Error
}

// DeleteByRepository forgets a deleted repository's list.
func (r *RepositoryAgentListRepository) DeleteByRepository(ctx context.Context, repositoryID string) error {
	return r.db.WithContext(ctx).Delete(&RepositoryAgentListModel{}, "repository_id = ?", repositoryID).Error
}

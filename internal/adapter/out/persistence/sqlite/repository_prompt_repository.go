package sqlite

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.RepositoryPromptStore = (*RepositoryPromptRepository)(nil)

// RepositoryPromptRepository stores the repository prompts.
type RepositoryPromptRepository struct{ db *gorm.DB }

// NewRepositoryPromptRepository returns the store.
func NewRepositoryPromptRepository(db *gorm.DB) *RepositoryPromptRepository {
	return &RepositoryPromptRepository{db: db}
}

func (m RepositoryPromptModel) toDomain() *domain.RepositoryPrompt {
	return &domain.RepositoryPrompt{RepositoryID: m.RepositoryID, Agent: m.Agent, Content: m.Content, Version: m.Version}
}

// Get returns the entry, or an empty one with version "" when none.
func (r *RepositoryPromptRepository) Get(ctx context.Context, repositoryID, agent string) (*domain.RepositoryPrompt, error) {
	var m RepositoryPromptModel
	if err := r.db.WithContext(ctx).Limit(1).Find(&m, "repository_id = ? AND agent = ?", repositoryID, agent).Error; err != nil {
		return nil, err
	}
	if m.RepositoryID == "" {
		return &domain.RepositoryPrompt{RepositoryID: repositoryID, Agent: agent}, nil
	}
	return m.toDomain(), nil
}

// ListByRepository returns the repository's entries, by agent name.
func (r *RepositoryPromptRepository) ListByRepository(ctx context.Context, repositoryID string) ([]*domain.RepositoryPrompt, error) {
	var models []RepositoryPromptModel
	if err := r.db.WithContext(ctx).Where("repository_id = ?", repositoryID).Order("agent").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.RepositoryPrompt, 0, len(models))
	for _, m := range models {
		out = append(out, m.toDomain())
	}
	return out, nil
}

// Put creates or replaces the entry.
func (r *RepositoryPromptRepository) Put(ctx context.Context, p *domain.RepositoryPrompt) error {
	m := RepositoryPromptModel{RepositoryID: p.RepositoryID, Agent: p.Agent, Content: p.Content, Version: p.Version}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "repository_id"}, {Name: "agent"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "version"}),
	}).Create(&m).Error
}

// Delete removes the entry.
func (r *RepositoryPromptRepository) Delete(ctx context.Context, repositoryID, agent string) error {
	return r.db.WithContext(ctx).Delete(&RepositoryPromptModel{}, "repository_id = ? AND agent = ?", repositoryID, agent).Error
}

// repositoriesOf are the repositories holding an entry of the agent.
func (r *RepositoryPromptRepository) repositoriesOf(tx *gorm.DB, agent string) ([]string, error) {
	var ids []string
	err := tx.Model(&RepositoryPromptModel{}).Where("agent = ?", agent).Order("repository_id").Pluck("repository_id", &ids).Error
	return ids, err
}

// Rename moves every repository's entry of the agent to its new name.
func (r *RepositoryPromptRepository) Rename(ctx context.Context, from, to string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if ids, err = r.repositoriesOf(tx, from); err != nil || len(ids) == 0 {
			return err
		}
		// An entry already under the new name (the name of a deleted agent that
		// was left behind) gives way.
		if err := tx.Where("agent = ?", to).Delete(&RepositoryPromptModel{}).Error; err != nil {
			return err
		}
		return tx.Model(&RepositoryPromptModel{}).Where("agent = ?", from).Update("agent", to).Error
	})
	return ids, err
}

// DeleteAgent removes every repository's entry of the agent.
func (r *RepositoryPromptRepository) DeleteAgent(ctx context.Context, agent string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		if ids, err = r.repositoriesOf(tx, agent); err != nil || len(ids) == 0 {
			return err
		}
		return tx.Where("agent = ?", agent).Delete(&RepositoryPromptModel{}).Error
	})
	return ids, err
}

// DeleteByRepository forgets a deleted repository's entries.
func (r *RepositoryPromptRepository) DeleteByRepository(ctx context.Context, repositoryID string) error {
	return r.db.WithContext(ctx).Delete(&RepositoryPromptModel{}, "repository_id = ?", repositoryID).Error
}

// DeleteOrphans forgets the entries of repositories that no longer exist.
func (r *RepositoryPromptRepository) DeleteOrphans(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("repository_id NOT IN (SELECT id FROM repositories)").Delete(&RepositoryPromptModel{}).Error
}

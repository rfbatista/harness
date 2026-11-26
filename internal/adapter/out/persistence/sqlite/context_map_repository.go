package sqlite

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

var _ ports.ContextMapStore = (*ContextMapRepository)(nil)

// ContextMapModel is the GORM model of a repository's generated context map.
type ContextMapModel struct {
	RepositoryID string `gorm:"primaryKey;column:repository_id"`
	Commit       string `gorm:"column:commit_sha"`
	Content      string
	GeneratedAt  time.Time
}

// TableName overrides the table name.
func (ContextMapModel) TableName() string { return "context_maps" }

// ContextMapRepository persists the context maps.
type ContextMapRepository struct{ db *gorm.DB }

// NewContextMapRepository returns a new context map store.
func NewContextMapRepository(db *gorm.DB) *ContextMapRepository { return &ContextMapRepository{db: db} }

// GetContextMap implements ports.ContextMapStore.
func (r *ContextMapRepository) GetContextMap(ctx context.Context, repositoryID string) (*ports.StoredContextMap, error) {
	var m ContextMapModel
	err := r.db.WithContext(ctx).Where("repository_id = ?", repositoryID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ports.StoredContextMap{RepositoryID: m.RepositoryID, Commit: m.Commit, Content: m.Content, GeneratedAt: m.GeneratedAt}, nil
}

// PutContextMap implements ports.ContextMapStore.
func (r *ContextMapRepository) PutContextMap(ctx context.Context, m ports.StoredContextMap) error {
	row := ContextMapModel{RepositoryID: m.RepositoryID, Commit: m.Commit, Content: m.Content, GeneratedAt: m.GeneratedAt}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}

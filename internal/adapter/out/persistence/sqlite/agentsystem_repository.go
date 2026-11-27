package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AgentSystemStore = (*AgentSystemRepository)(nil)

// AgentSystemSettingModel is one field one level sets (ADR 0011): a row per
// field, so a reset removes exactly one value. Level is "project" or
// "repository", OwnerID the project or repository id.
type AgentSystemSettingModel struct {
	Level     string `gorm:"primaryKey"`
	OwnerID   string `gorm:"column:owner_id;primaryKey"`
	Field     string `gorm:"primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Value     string
	UpdatedAt time.Time
}

// TableName overrides the table name.
func (AgentSystemSettingModel) TableName() string { return "agent_system_settings" }

// AgentSystemSaveModel is when an owner's level was last saved: a save that
// only resets removes rows, so the rows alone cannot tell.
type AgentSystemSaveModel struct {
	Level   string `gorm:"primaryKey"`
	OwnerID   string `gorm:"column:owner_id;primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	SavedAt   time.Time
}

// TableName overrides the table name.
func (AgentSystemSaveModel) TableName() string { return "agent_system_saves" }

// AgentSystemRepository stores the agent system settings.
type AgentSystemRepository struct{ db *gorm.DB }

// NewAgentSystemRepository returns the store.
func NewAgentSystemRepository(db *gorm.DB) *AgentSystemRepository {
	return &AgentSystemRepository{db: db}
}

// Values returns what the owner sets.
func (r *AgentSystemRepository) Values(ctx context.Context, level domain.AgentSystemSource, ownerID string) (domain.AgentSystemValues, error) {
	var rows []AgentSystemSettingModel
	if err := r.db.WithContext(ctx).Where("level = ? AND owner_id = ?", level, ownerID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(domain.AgentSystemValues, len(rows))
	for _, row := range rows {
		out[row.Field] = json.RawMessage(row.Value)
	}
	return out, nil
}

// Apply stores set and removes reset in one transaction.
func (r *AgentSystemRepository) Apply(ctx context.Context, level domain.AgentSystemSource, ownerID, projectID string, set domain.AgentSystemValues, reset []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for field, value := range set {
			row := AgentSystemSettingModel{Level: string(level), OwnerID: ownerID, Field: field, ProjectID: projectID, Value: string(value), UpdatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "level"}, {Name: "owner_id"}, {Name: "field"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "project_id", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		if len(reset) > 0 {
			if err := tx.Where("level = ? AND owner_id = ? AND field IN ?", level, ownerID, reset).Delete(&AgentSystemSettingModel{}).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "level"}, {Name: "owner_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"saved_at", "project_id"}),
		}).Create(&AgentSystemSaveModel{Level: string(level), OwnerID: ownerID, ProjectID: projectID, SavedAt: time.Now()}).Error
	})
}

// LastSave returns when the owner's level was last saved, nil when never.
func (r *AgentSystemRepository) LastSave(ctx context.Context, level domain.AgentSystemSource, ownerID string) (*time.Time, error) {
	var row AgentSystemSaveModel
	res := r.db.WithContext(ctx).Where("level = ? AND owner_id = ?", level, ownerID).Limit(1).Find(&row)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &row.SavedAt, nil
}

// DeleteOwner removes everything the owner sets.
func (r *AgentSystemRepository) DeleteOwner(ctx context.Context, level domain.AgentSystemSource, ownerID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("level = ? AND owner_id = ?", level, ownerID).Delete(&AgentSystemSaveModel{}).Error; err != nil {
			return err
		}
		return tx.Where("level = ? AND owner_id = ?", level, ownerID).Delete(&AgentSystemSettingModel{}).Error
	})
}

// DeleteProject removes everything the project and its repositories set.
func (r *AgentSystemRepository) DeleteProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", projectID).Delete(&AgentSystemSaveModel{}).Error; err != nil {
			return err
		}
		return tx.Where("project_id = ?", projectID).Delete(&AgentSystemSettingModel{}).Error
	})
}

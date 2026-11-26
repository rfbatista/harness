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
			return tx.Where("level = ? AND owner_id = ? AND field IN ?", level, ownerID, reset).Delete(&AgentSystemSettingModel{}).Error
		}
		return nil
	})
}

// DeleteOwner removes everything the owner sets.
func (r *AgentSystemRepository) DeleteOwner(ctx context.Context, level domain.AgentSystemSource, ownerID string) error {
	return r.db.WithContext(ctx).Where("level = ? AND owner_id = ?", level, ownerID).Delete(&AgentSystemSettingModel{}).Error
}

// DeleteProject removes everything the project and its repositories set.
func (r *AgentSystemRepository) DeleteProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&AgentSystemSettingModel{}).Error
}

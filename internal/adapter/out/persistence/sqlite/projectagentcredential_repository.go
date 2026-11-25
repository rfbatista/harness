package sqlite

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

// ProjectAgentCredentialModel is a project's own agent credential. Like the
// global one it is a table of its own, not a setting, because settings are
// served whole to the page.
type ProjectAgentCredentialModel struct {
	ProjectID string `gorm:"column:project_id;primaryKey"`
	Kind      string `gorm:"column:kind"`
	Sealed    []byte `gorm:"column:sealed"`
	KeyID     string `gorm:"column:key_id"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (ProjectAgentCredentialModel) TableName() string { return "project_agent_credential" }

// ProjectAgentCredentialRepository implements ports.ProjectAgentCredentialStore.
type ProjectAgentCredentialRepository struct{ db *gorm.DB }

var _ ports.ProjectAgentCredentialStore = (*ProjectAgentCredentialRepository)(nil)

func NewProjectAgentCredentialRepository(db *gorm.DB) *ProjectAgentCredentialRepository {
	return &ProjectAgentCredentialRepository{db: db}
}

func (r *ProjectAgentCredentialRepository) GetFor(projectID string) (*ports.AgentCredentialRecord, error) {
	var m ProjectAgentCredentialModel
	err := r.db.Where("project_id = ?", projectID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ports.AgentCredentialRecord{Kind: m.Kind, Sealed: m.Sealed, KeyID: m.KeyID, Updated: time.Unix(m.UpdatedAt, 0).UTC()}, nil
}

func (r *ProjectAgentCredentialRepository) PutFor(projectID string, rec ports.AgentCredentialRecord) error {
	return r.db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&ProjectAgentCredentialModel{ProjectID: projectID, Kind: rec.Kind, Sealed: rec.Sealed, KeyID: rec.KeyID, UpdatedAt: rec.Updated.Unix()}).Error
}

func (r *ProjectAgentCredentialRepository) DeleteFor(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&ProjectAgentCredentialModel{}).Error
}

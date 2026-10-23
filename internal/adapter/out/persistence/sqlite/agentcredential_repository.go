package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

// AgentCredentialModel is the one agent credential; its id is always 1. The
// value is in Sealed, encrypted with the key KeyID names. It is a table of
// its own, not a setting, because settings are served whole to the page.
type AgentCredentialModel struct {
	ID        int    `gorm:"primaryKey"`
	Kind      string `gorm:"column:kind"`
	Sealed    []byte `gorm:"column:sealed"`
	KeyID     string `gorm:"column:key_id"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (AgentCredentialModel) TableName() string { return "agent_credential" }

// AgentCredentialRepository implements ports.AgentCredentialStore.
type AgentCredentialRepository struct{ db *gorm.DB }

var _ ports.AgentCredentialStore = (*AgentCredentialRepository)(nil)

func NewAgentCredentialRepository(db *gorm.DB) *AgentCredentialRepository {
	return &AgentCredentialRepository{db: db}
}

func (r *AgentCredentialRepository) Get() (*ports.AgentCredentialRecord, error) {
	var ms []AgentCredentialModel
	if err := r.db.Where("id = 1").Limit(1).Find(&ms).Error; err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, nil
	}
	m := ms[0]
	return &ports.AgentCredentialRecord{Kind: m.Kind, Sealed: m.Sealed, KeyID: m.KeyID, Updated: time.Unix(m.UpdatedAt, 0).UTC()}, nil
}

func (r *AgentCredentialRepository) Put(rec ports.AgentCredentialRecord) error {
	return r.db.Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&AgentCredentialModel{ID: 1, Kind: rec.Kind, Sealed: rec.Sealed, KeyID: rec.KeyID, UpdatedAt: rec.Updated.Unix()}).Error
}

func (r *AgentCredentialRepository) Delete() error {
	return r.db.Where("id = 1").Delete(&AgentCredentialModel{}).Error
}

package sqlite

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GuardSessions = (*GuardSessionRepository)(nil)

// GuardSessionModel is a session's kept guard configuration, as JSON.
type GuardSessionModel struct {
	SessionID string `gorm:"column:session_id;primaryKey"`
	Config    string `gorm:"column:config"`
}

func (GuardSessionModel) TableName() string { return "guard_sessions" }

type GuardSessionRepository struct{ db *gorm.DB }

func NewGuardSessionRepository(db *gorm.DB) *GuardSessionRepository {
	return &GuardSessionRepository{db: db}
}

func (r *GuardSessionRepository) Keep(ctx context.Context, sessionID string, cfg domain.GuardConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "session_id"}}, DoUpdates: clause.AssignmentColumns([]string{"config"}),
	}).Create(&GuardSessionModel{SessionID: sessionID, Config: string(b)}).Error
}

func (r *GuardSessionRepository) Config(ctx context.Context, sessionID string) (domain.GuardConfig, bool, error) {
	var m GuardSessionModel
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Limit(1).Find(&m).Error
	if err != nil || m.SessionID == "" {
		return domain.GuardConfig{}, false, err
	}
	var cfg domain.GuardConfig
	if err := json.Unmarshal([]byte(m.Config), &cfg); err != nil {
		return domain.GuardConfig{}, false, err
	}
	return cfg, true, nil
}

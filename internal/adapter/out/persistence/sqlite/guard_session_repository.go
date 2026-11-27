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
	// RedRuns is the consecutive red declaration runs since the last green one
	// or the last start or resume.
	RedRuns int `gorm:"column:red_runs"`
	// Escalated says the session was stopped at its retry limit and has not been
	// resumed since.
	Escalated bool `gorm:"column:escalated"`
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

func (r *GuardSessionRepository) CountRed(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&GuardSessionModel{}).Where("session_id = ?", sessionID).
			UpdateColumn("red_runs", gorm.Expr("red_runs + 1")).Error; err != nil {
			return err
		}
		return tx.Model(&GuardSessionModel{}).Where("session_id = ?", sessionID).Select("red_runs").Scan(&n).Error
	})
	return n, err
}

func (r *GuardSessionRepository) ResetRed(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Model(&GuardSessionModel{}).Where("session_id = ?", sessionID).UpdateColumn("red_runs", 0).Error
}

func (r *GuardSessionRepository) MarkEscalated(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Model(&GuardSessionModel{}).Where("session_id = ?", sessionID).UpdateColumn("escalated", true).Error
}

func (r *GuardSessionRepository) ResumeRed(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Model(&GuardSessionModel{}).Where("session_id = ? AND escalated = ?", sessionID, true).
		UpdateColumns(map[string]any{"red_runs": 0, "escalated": false}).Error
}

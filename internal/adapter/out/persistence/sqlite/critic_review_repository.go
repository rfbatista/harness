package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/ports"
)

var _ ports.CriticReviewStore = (*CriticReviewRepository)(nil)

// CriticReviewModel is one diff a critic was launched for.
type CriticReviewModel struct {
	SessionID string `gorm:"column:session_id;primaryKey"`
	Base      string `gorm:"column:base;primaryKey"`
	Head      string `gorm:"column:head;primaryKey"`
	GateRunID string `gorm:"column:gate_run_id"`
	CreatedMs int64  `gorm:"column:created_ms"`
}

func (CriticReviewModel) TableName() string { return "critic_reviews" }

type CriticReviewRepository struct{ db *gorm.DB }

func NewCriticReviewRepository(db *gorm.DB) *CriticReviewRepository {
	return &CriticReviewRepository{db: db}
}

func (r *CriticReviewRepository) Seen(ctx context.Context, sessionID, base, head string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&CriticReviewModel{}).
		Where("session_id = ? AND base = ? AND head = ?", sessionID, base, head).Count(&n).Error
	return n > 0, err
}

func (r *CriticReviewRepository) Claim(ctx context.Context, sessionID, base, head, gateRunID string) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&CriticReviewModel{
		SessionID: sessionID, Base: base, Head: head, GateRunID: gateRunID, CreatedMs: time.Now().UnixMilli(),
	})
	return res.RowsAffected == 1, res.Error
}

func (r *CriticReviewRepository) Release(ctx context.Context, sessionID, base, head string) error {
	return r.db.WithContext(ctx).
		Where("session_id = ? AND base = ? AND head = ?", sessionID, base, head).Delete(&CriticReviewModel{}).Error
}

package sqlite

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GateRunStore = (*GateRunRepository)(nil)

// GateRunModel is one gate run; Data is the run as JSON, the columns are what
// it is looked up by.
type GateRunModel struct {
	ID        string `gorm:"column:id;primaryKey"`
	SessionID string `gorm:"column:session_id;index"`
	StartedMs int64  `gorm:"column:started_ms"`
	Data      string `gorm:"column:data"`
}

func (GateRunModel) TableName() string { return "gate_runs" }

type GateRunRepository struct{ db *gorm.DB }

func NewGateRunRepository(db *gorm.DB) *GateRunRepository { return &GateRunRepository{db: db} }

func (r *GateRunRepository) Save(ctx context.Context, run *domain.GateRun) error {
	b, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"data"}),
	}).Create(&GateRunModel{ID: run.ID, SessionID: run.SessionID, StartedMs: run.StartedAt.UnixMilli(), Data: string(b)}).Error
}

func (r *GateRunRepository) List(ctx context.Context, sessionID string) ([]domain.GateRun, error) {
	var rows []GateRunModel
	if err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Order("started_ms, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.GateRun, 0, len(rows))
	for _, m := range rows {
		var run domain.GateRun
		if err := json.Unmarshal([]byte(m.Data), &run); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

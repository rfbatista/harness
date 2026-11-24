package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionContextStore = (*SessionContextRepository)(nil)

// SessionContextModel is a session's stored context: its entries, usage and
// changes as one JSON document, since it is only ever read and written whole.
type SessionContextModel struct {
	SessionID string `gorm:"primaryKey;column:session_id"`
	Data      string
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName overrides the table name.
func (SessionContextModel) TableName() string { return "session_contexts" }

// SessionContextRepository persists session contexts.
type SessionContextRepository struct{ db *gorm.DB }

// NewSessionContextRepository returns the repository.
func NewSessionContextRepository(db *gorm.DB) *SessionContextRepository {
	return &SessionContextRepository{db: db}
}

// Load implements ports.SessionContextStore.
func (r *SessionContextRepository) Load(ctx context.Context, sessionID string) (*domain.SessionContext, error) {
	var m SessionContextModel
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c domain.SessionContext
	if err := json.Unmarshal([]byte(m.Data), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save implements ports.SessionContextStore.
func (r *SessionContextRepository) Save(ctx context.Context, c *domain.SessionContext) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	m := SessionContextModel{SessionID: c.SessionID, Data: string(b), UpdatedAt: time.Now()}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		UpdateAll: true,
	}).Create(&m).Error
}

// Delete implements ports.SessionContextStore.
func (r *SessionContextRepository) Delete(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&SessionContextModel{}).Error
}

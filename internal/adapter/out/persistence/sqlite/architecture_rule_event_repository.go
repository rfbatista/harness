package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ArchitectureRuleEventStore = (*ArchitectureRuleEventRepository)(nil)

// ArchitectureRuleEventModel is the GORM model of domain.ArchitectureRuleEvent.
type ArchitectureRuleEventModel struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"column:repository_id;index:idx_rule_events_repo_at,priority:1"`
	ProjectID    string `gorm:"column:project_id"`
	SessionID    string `gorm:"column:session_id;index"`
	TaskID       string `gorm:"column:task_id"`
	Agent        string
	RuleID       string `gorm:"column:rule_id"`
	Kind         string
	AtPoint      string `gorm:"column:at_point"`
	Mode         string
	Outcome      string
	Path         string
	Layer        string
	Tool         string
	Detail       string
	At           time.Time `gorm:"index:idx_rule_events_repo_at,priority:2"`
}

// TableName overrides the table name.
func (ArchitectureRuleEventModel) TableName() string { return "architecture_rule_events" }

// ArchitectureRuleEventRepository persists rule events in SQLite via GORM.
type ArchitectureRuleEventRepository struct {
	db *gorm.DB
}

// NewArchitectureRuleEventRepository returns a new event repository.
func NewArchitectureRuleEventRepository(db *gorm.DB) *ArchitectureRuleEventRepository {
	return &ArchitectureRuleEventRepository{db: db}
}

// Add implements ports.ArchitectureRuleEventStore; it sets the event's id.
func (r *ArchitectureRuleEventRepository) Add(ctx context.Context, ev *domain.ArchitectureRuleEvent) error {
	id, err := genID()
	if err != nil {
		return err
	}
	ev.ID = id
	m := ArchitectureRuleEventModel{
		ID: id, RepositoryID: ev.RepositoryID, ProjectID: ev.ProjectID, SessionID: ev.SessionID, TaskID: ev.TaskID, Agent: ev.Agent,
		RuleID: ev.RuleID, Kind: ev.Kind, AtPoint: ev.AtPoint, Mode: ev.Mode, Outcome: ev.Outcome, Path: ev.Path,
		Layer: ev.Layer, Tool: ev.Tool, Detail: ev.Detail, At: ev.At,
	}
	return r.db.WithContext(ctx).Create(&m).Error
}

// List implements ports.ArchitectureRuleEventStore.
func (r *ArchitectureRuleEventRepository) List(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error) {
	q := r.db.WithContext(ctx).Where("repository_id = ?", repositoryID)
	if !since.IsZero() {
		q = q.Where("at > ?", since)
	}
	var rows []ArchitectureRuleEventModel
	if err := q.Order("at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ArchitectureRuleEvent, len(rows))
	for i, m := range rows {
		out[i] = domain.ArchitectureRuleEvent{
			ID: m.ID, At: m.At, SessionID: m.SessionID, TaskID: m.TaskID, Agent: m.Agent, RuleID: m.RuleID, Kind: m.Kind,
			AtPoint: m.AtPoint, Mode: m.Mode, Outcome: m.Outcome, Path: m.Path, Layer: m.Layer, Tool: m.Tool, Detail: m.Detail,
			RepositoryID: m.RepositoryID, ProjectID: m.ProjectID,
		}
	}
	return out, nil
}

// DeleteBySession implements ports.ArchitectureRuleEventStore.
func (r *ArchitectureRuleEventRepository) DeleteBySession(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&ArchitectureRuleEventModel{}).Error
}

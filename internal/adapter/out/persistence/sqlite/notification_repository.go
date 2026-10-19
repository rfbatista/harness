package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// NotificationModel is the GORM model for domain.Notification. Names are not
// stored: the service reads the project's and task's current ones.
type NotificationModel struct {
	ID         string `gorm:"primaryKey"`
	ProjectID  string `gorm:"column:project_id;index"`
	TaskID     string `gorm:"column:task_id;index"`
	SessionID  string `gorm:"column:session_id;index"`
	Source     string `gorm:"column:source"`
	Kind       string `gorm:"column:kind"`
	Level      string `gorm:"column:level"`
	Title      string `gorm:"column:title"`
	Body       string `gorm:"column:body"`
	Link       string `gorm:"column:link"`
	CauseKey   string `gorm:"column:cause_key;index"`
	CreatedAt  int64  `gorm:"column:created_at;index:idx_notifications_created"`
	ReadAt     int64  `gorm:"column:read_at"`
	ResolvedAt int64  `gorm:"column:resolved_at"`
}

func (NotificationModel) TableName() string { return "notifications" }

func (m *NotificationModel) ToDomain() *domain.Notification {
	return &domain.Notification{
		ID: m.ID, ProjectID: m.ProjectID, TaskID: m.TaskID, SessionID: m.SessionID,
		Source: domain.NotificationSource(m.Source), Kind: domain.NotificationKind(m.Kind), Level: domain.NotificationLevel(m.Level),
		Title: m.Title, Body: m.Body, Link: m.Link, CauseKey: m.CauseKey,
		CreatedAt: time.UnixMilli(m.CreatedAt), ReadAt: timeFromMilli(m.ReadAt), ResolvedAt: timeFromMilli(m.ResolvedAt),
	}
}

// NotificationRepository implements ports.NotificationRepository.
type NotificationRepository struct{ db *gorm.DB }

var _ ports.NotificationRepository = (*NotificationRepository)(nil)

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

const unreadWhere = "read_at = 0 AND resolved_at = 0"

func (r *NotificationRepository) Create(n *domain.Notification) (*domain.Notification, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	created := n.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	m := &NotificationModel{
		ID: id, ProjectID: n.ProjectID, TaskID: n.TaskID, SessionID: n.SessionID,
		Source: string(n.Source), Kind: string(n.Kind), Level: string(n.Level),
		Title: n.Title, Body: n.Body, Link: n.Link, CauseKey: n.CauseKey,
		CreatedAt: created.UnixMilli(), ReadAt: milliOf(n.ReadAt), ResolvedAt: milliOf(n.ResolvedAt),
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *NotificationRepository) Get(id string) *domain.Notification {
	var ms []NotificationModel
	if err := r.db.Where("id = ?", id).Limit(1).Find(&ms).Error; err != nil || len(ms) == 0 {
		return nil
	}
	return ms[0].ToDomain()
}

func (r *NotificationRepository) OpenByCause(causeKey string) *domain.Notification {
	var ms []NotificationModel
	if err := r.db.Where("cause_key = ? AND resolved_at = 0", causeKey).Order("created_at DESC, id DESC").Limit(1).Find(&ms).Error; err != nil || len(ms) == 0 {
		return nil
	}
	return ms[0].ToDomain()
}

func (r *NotificationRepository) AnyByCause(causeKey string) bool {
	var n int64
	r.db.Model(&NotificationModel{}).Where("cause_key = ?", causeKey).Limit(1).Count(&n)
	return n > 0
}

func (r *NotificationRepository) List(ctx context.Context, f ports.NotificationFilter) ([]*domain.Notification, error) {
	q := r.db.WithContext(ctx).Model(&NotificationModel{})
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	if f.UnreadOnly {
		q = q.Where(unreadWhere)
	}
	if !f.BeforeAt.IsZero() {
		at := f.BeforeAt.UnixMilli()
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", at, at, f.BeforeID)
	}
	if !f.Since.IsZero() {
		q = q.Where("created_at > ?", f.Since.UnixMilli())
	}
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	var ms []NotificationModel
	if err := q.Order("created_at DESC, id DESC").Find(&ms).Error; err != nil {
		return nil, err
	}
	return toNotifications(ms), nil
}

func (r *NotificationRepository) CountUnread(ctx context.Context) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&NotificationModel{}).Where(unreadWhere).Count(&n).Error
	return int(n), err
}

func (r *NotificationRepository) MarkRead(ids []string, at time.Time) ([]*domain.Notification, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.change(r.db.Where("id IN ? AND read_at = 0", ids), "read_at", at)
}

func (r *NotificationRepository) MarkAllRead(projectID string, at time.Time) ([]*domain.Notification, error) {
	q := r.db.Where(unreadWhere)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	return r.change(q, "read_at", at)
}

func (r *NotificationRepository) Resolve(causeKey string, at time.Time) ([]*domain.Notification, error) {
	return r.change(r.db.Where("cause_key = ? AND resolved_at = 0", causeKey), "resolved_at", at)
}

func (r *NotificationRepository) ResolveTask(taskID string, at time.Time) ([]*domain.Notification, error) {
	return r.change(r.db.Where("task_id = ? AND resolved_at = 0 AND cause_key <> ''", taskID), "resolved_at", at)
}

// change sets column to at on the rows where selects, and answers them as
// they now are.
func (r *NotificationRepository) change(where *gorm.DB, column string, at time.Time) ([]*domain.Notification, error) {
	var out []*domain.Notification
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var ids []string
		if err := tx.Model(&NotificationModel{}).Where(where).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
			return err
		}
		if err := tx.Model(&NotificationModel{}).Where("id IN ?", ids).Update(column, at.UnixMilli()).Error; err != nil {
			return err
		}
		var ms []NotificationModel
		if err := tx.Where("id IN ?", ids).Order("created_at DESC, id DESC").Find(&ms).Error; err != nil {
			return err
		}
		out = toNotifications(ms)
		return nil
	})
	return out, err
}

func (r *NotificationRepository) CountAgentSince(sessionID, projectID string, since time.Time) (int, error) {
	q := r.db.Model(&NotificationModel{}).Where("source = ? AND created_at > ?", string(domain.NotificationFromAgent), since.UnixMilli())
	if sessionID != "" {
		q = q.Where("session_id = ?", sessionID)
	} else {
		q = q.Where("project_id = ? AND session_id = ''", projectID)
	}
	var n int64
	err := q.Count(&n).Error
	return int(n), err
}

func (r *NotificationRepository) DeleteByProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&NotificationModel{}).Error
}

func (r *NotificationRepository) Prune(before time.Time) (int, error) {
	res := r.db.Where("created_at < ? AND (read_at <> 0 OR resolved_at <> 0)", before.UnixMilli()).Delete(&NotificationModel{})
	return int(res.RowsAffected), res.Error
}

// Transaction runs fn in one transaction on the writer connection, which
// takes the write lock when it begins.
func (r *NotificationRepository) Transaction(fn func(tx ports.NotificationRepository) error) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return fn(&NotificationRepository{db: tx})
	})
}

func toNotifications(ms []NotificationModel) []*domain.Notification {
	out := make([]*domain.Notification, 0, len(ms))
	for i := range ms {
		out = append(out, ms[i].ToDomain())
	}
	return out
}

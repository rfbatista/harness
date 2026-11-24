package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ProductOwnerStore = (*ProductOwnerRepository)(nil)

// ProductOwnerSettingModel is the GORM model of a project's product owner
// settings. It stores what is switched off, so a project with no row has the
// defaults.
type ProductOwnerSettingModel struct {
	ProjectID  string `gorm:"primaryKey;column:project_id"`
	Disabled   bool
	AgentID    string `gorm:"column:agent_id"`
	Prompt     string
	DigestTime string `gorm:"column:digest_time"`
}

// TableName overrides the table name.
func (ProductOwnerSettingModel) TableName() string { return "product_owner_settings" }

// ProductOwnerThreadModel is the GORM model of domain.ProductOwnerThread.
type ProductOwnerThreadModel struct {
	ID                 string `gorm:"primaryKey"`
	ProjectID          string `gorm:"column:project_id;index"`
	SessionID          string `gorm:"column:session_id;index"`
	Title              string
	Turn               string `gorm:"index"`
	StartedAt          time.Time
	LastActivityAt     time.Time
	PendingHeldActions int    `gorm:"column:pending_held_actions"`
	AgentID            string `gorm:"column:agent_id"`
	Prompt             string
}

// TableName overrides the table name.
func (ProductOwnerThreadModel) TableName() string { return "product_owner_threads" }

// ProductOwnerItemModel is one item of a thread. Seq orders a thread's items;
// Body holds the item as JSON, so a later kind adds fields with no migration.
type ProductOwnerItemModel struct {
	Seq      int64  `gorm:"primaryKey;autoIncrement"`
	ID       string `gorm:"uniqueIndex"`
	ThreadID string `gorm:"column:thread_id;index"`
	Body     string
}

// TableName overrides the table name.
func (ProductOwnerItemModel) TableName() string { return "product_owner_items" }

// ProductOwnerRepository persists product owner settings, threads and thread
// items in SQLite via GORM.
type ProductOwnerRepository struct{ db *gorm.DB }

// NewProductOwnerRepository returns a new product owner repository.
func NewProductOwnerRepository(db *gorm.DB) *ProductOwnerRepository {
	return &ProductOwnerRepository{db: db}
}

// Settings implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) Settings(ctx context.Context, projectID string) (domain.ProductOwnerSettings, error) {
	var m ProductOwnerSettingModel
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.DefaultProductOwnerSettings(projectID), nil
	}
	if err != nil {
		return domain.ProductOwnerSettings{}, err
	}
	return domain.ProductOwnerSettings{ProjectID: m.ProjectID, Enabled: !m.Disabled, AgentID: m.AgentID, Prompt: m.Prompt, DigestTime: m.DigestTime}, nil
}

// SaveSettings implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) SaveSettings(ctx context.Context, s domain.ProductOwnerSettings) error {
	m := ProductOwnerSettingModel{ProjectID: s.ProjectID, Disabled: !s.Enabled, AgentID: s.AgentID, Prompt: s.Prompt, DigestTime: s.DigestTime}
	return r.db.WithContext(ctx).Save(&m).Error
}

func threadModel(t domain.ProductOwnerThread) ProductOwnerThreadModel {
	return ProductOwnerThreadModel{ID: t.ID, ProjectID: t.ProjectID, SessionID: t.SessionID, Title: t.Title, Turn: t.Turn,
		StartedAt: t.StartedAt, LastActivityAt: t.LastActivityAt, PendingHeldActions: t.PendingHeldActions, AgentID: t.AgentID, Prompt: t.Prompt}
}

func (m ProductOwnerThreadModel) domain() domain.ProductOwnerThread {
	return domain.ProductOwnerThread{ID: m.ID, ProjectID: m.ProjectID, SessionID: m.SessionID, Title: m.Title, Turn: m.Turn,
		StartedAt: m.StartedAt, LastActivityAt: m.LastActivityAt, PendingHeldActions: m.PendingHeldActions, AgentID: m.AgentID, Prompt: m.Prompt}
}

// CreateThread implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) CreateThread(ctx context.Context, t domain.ProductOwnerThread) error {
	m := threadModel(t)
	return r.db.WithContext(ctx).Create(&m).Error
}

// Thread implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) Thread(ctx context.Context, id string) (*domain.ProductOwnerThread, error) {
	var m ProductOwnerThreadModel
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &domain.StructuredError{Code: "THREAD_NOT_FOUND", Message: "THREAD_NOT_FOUND: no product owner thread " + id}
	}
	if err != nil {
		return nil, err
	}
	t := m.domain()
	return &t, nil
}

// ThreadOfSession implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) ThreadOfSession(ctx context.Context, sessionID string) (*domain.ProductOwnerThread, error) {
	var m ProductOwnerThreadModel
	err := r.db.WithContext(ctx).Where("session_id = ?", sessionID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := m.domain()
	return &t, nil
}

func threads(ms []ProductOwnerThreadModel) []domain.ProductOwnerThread {
	out := make([]domain.ProductOwnerThread, len(ms))
	for i, m := range ms {
		out[i] = m.domain()
	}
	return out
}

// Threads implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) Threads(ctx context.Context, projectID string) ([]domain.ProductOwnerThread, error) {
	var ms []ProductOwnerThreadModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("last_activity_at DESC, id DESC").Find(&ms).Error; err != nil {
		return nil, err
	}
	return threads(ms), nil
}

// ThreadsWithTurn implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) ThreadsWithTurn(ctx context.Context, turn string) ([]domain.ProductOwnerThread, error) {
	var ms []ProductOwnerThreadModel
	if err := r.db.WithContext(ctx).Where("turn = ?", turn).Order("started_at").Find(&ms).Error; err != nil {
		return nil, err
	}
	return threads(ms), nil
}

// SaveThread implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) SaveThread(ctx context.Context, t domain.ProductOwnerThread) error {
	m := threadModel(t)
	return r.db.WithContext(ctx).Save(&m).Error
}

func itemModel(item domain.ProductOwnerItem) (ProductOwnerItemModel, error) {
	body, err := json.Marshal(item)
	if err != nil {
		return ProductOwnerItemModel{}, err
	}
	return ProductOwnerItemModel{Seq: item.Seq, ID: item.ID, ThreadID: item.ThreadID, Body: string(body)}, nil
}

func (m ProductOwnerItemModel) domain() (domain.ProductOwnerItem, error) {
	var item domain.ProductOwnerItem
	if err := json.Unmarshal([]byte(m.Body), &item); err != nil {
		return item, err
	}
	item.ID, item.ThreadID, item.Seq = m.ID, m.ThreadID, m.Seq
	return item, nil
}

// AppendItem implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) AppendItem(ctx context.Context, item domain.ProductOwnerItem) (domain.ProductOwnerItem, error) {
	id, err := genID()
	if err != nil {
		return item, err
	}
	item.ID, item.Seq = id, 0
	m, err := itemModel(item)
	if err != nil {
		return item, err
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return item, err
	}
	item.Seq = m.Seq
	return item, nil
}

// SaveItem implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) SaveItem(ctx context.Context, item domain.ProductOwnerItem) error {
	m, err := itemModel(item)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&ProductOwnerItemModel{}).Where("id = ?", item.ID).Update("body", m.Body).Error
}

// Items implements ports.ProductOwnerStore.
func (r *ProductOwnerRepository) Items(ctx context.Context, threadID, beforeID string, limit int) ([]domain.ProductOwnerItem, bool, error) {
	q := r.db.WithContext(ctx).Where("thread_id = ?", threadID)
	if beforeID != "" {
		var before ProductOwnerItemModel
		if err := r.db.WithContext(ctx).Where("id = ? AND thread_id = ?", beforeID, threadID).Take(&before).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, false, &domain.StructuredError{Code: "INVALID_INPUT", Message: "before is not an item of this thread"}
			}
			return nil, false, err
		}
		q = q.Where("seq < ?", before.Seq)
	}
	var ms []ProductOwnerItemModel
	if err := q.Order("seq DESC").Limit(limit + 1).Find(&ms).Error; err != nil {
		return nil, false, err
	}
	hasEarlier := len(ms) > limit
	if hasEarlier {
		ms = ms[:limit]
	}
	out := make([]domain.ProductOwnerItem, len(ms))
	for i, m := range ms {
		item, err := m.domain()
		if err != nil {
			return nil, false, err
		}
		out[len(ms)-1-i] = item
	}
	return out, hasEarlier, nil
}

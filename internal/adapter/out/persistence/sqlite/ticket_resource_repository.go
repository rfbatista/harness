package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TicketResourceRepository = (*TicketResourceRepository)(nil)
	_ ports.TicketCommentRepository  = (*TicketCommentRepository)(nil)
)

// A task's resources and comments. Lists order by created_at, then rowid, so
// two rows made in the same millisecond keep the order they were made in.

// TicketResourceModel is the GORM model for domain.TicketResource. The
// content URL is derived, not stored.
type TicketResourceModel struct {
	ID        string `gorm:"primaryKey"`
	TicketID  string `gorm:"column:ticket_id;index:idx_ticket_resources_ticket_created"`
	ProjectID string `gorm:"column:project_id"`
	Kind      string `gorm:"column:kind"`
	Title     string `gorm:"column:title"`
	Mime      string `gorm:"column:mime"`
	SizeBytes int64  `gorm:"column:size_bytes"`
	URL       string `gorm:"column:url"`
	CreatedAt int64  `gorm:"column:created_at;index:idx_ticket_resources_ticket_created"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (TicketResourceModel) TableName() string { return "ticket_resources" }

func (m *TicketResourceModel) ToDomain() *domain.TicketResource {
	return &domain.TicketResource{
		ID: m.ID, TicketID: m.TicketID, ProjectID: m.ProjectID, Kind: domain.ResourceKind(m.Kind),
		Title: m.Title, Mime: m.Mime, SizeBytes: m.SizeBytes, URL: m.URL,
		CreatedAt: time.UnixMilli(m.CreatedAt), UpdatedAt: time.UnixMilli(m.UpdatedAt),
	}
}

// TicketCommentModel is the GORM model for domain.TicketComment.
type TicketCommentModel struct {
	ID        string `gorm:"primaryKey"`
	TicketID  string `gorm:"column:ticket_id;index:idx_ticket_comments_ticket_created"`
	ProjectID string `gorm:"column:project_id"`
	Author    string `gorm:"column:author"`
	SessionID string `gorm:"column:session_id;not null;default:''"`
	AgentName string `gorm:"column:agent_name;not null;default:''"`
	Body      string `gorm:"column:body"`
	CreatedAt int64  `gorm:"column:created_at;index:idx_ticket_comments_ticket_created"`
	UpdatedAt int64  `gorm:"column:updated_at"`
}

func (TicketCommentModel) TableName() string { return "ticket_comments" }

// ToDomain reads a row from before sessions could comment, with no author
// fields, as the person's.
func (m *TicketCommentModel) ToDomain() *domain.TicketComment {
	author := domain.CommentAuthor(m.Author)
	if author == "" {
		author = domain.CommentByPerson
	}
	return &domain.TicketComment{
		ID: m.ID, TicketID: m.TicketID, ProjectID: m.ProjectID, Author: author,
		SessionID: m.SessionID, AgentName: m.AgentName, Body: m.Body,
		CreatedAt: time.UnixMilli(m.CreatedAt), UpdatedAt: time.UnixMilli(m.UpdatedAt),
	}
}

// editedAt is the updated_at of a change made now: always later than
// created, so an edit made in the row's first millisecond still reads as one.
func editedAt(created int64) int64 {
	return max(time.Now().UnixMilli(), created+1)
}

// TicketResourceRepository persists resources in SQLite via GORM.
type TicketResourceRepository struct{ db *gorm.DB }

func NewTicketResourceRepository(db *gorm.DB) *TicketResourceRepository {
	return &TicketResourceRepository{db: db}
}

var errResourceNotFound = &domain.StructuredError{Code: "RESOURCE_NOT_FOUND", Message: "resource not found"}

func (r *TicketResourceRepository) Create(res *domain.TicketResource) (*domain.TicketResource, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	m := &TicketResourceModel{
		ID: id, TicketID: res.TicketID, ProjectID: res.ProjectID, Kind: string(res.Kind), Title: res.Title,
		Mime: res.Mime, SizeBytes: res.SizeBytes, URL: res.URL, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketResourceRepository) get(id string) *TicketResourceModel {
	var ms []TicketResourceModel
	if err := r.db.Where("id = ?", id).Limit(1).Find(&ms).Error; err != nil || len(ms) == 0 {
		return nil
	}
	return &ms[0]
}

func (r *TicketResourceRepository) Get(id string) *domain.TicketResource {
	if m := r.get(id); m != nil {
		return m.ToDomain()
	}
	return nil
}

func (r *TicketResourceRepository) ListByTicket(ctx context.Context, ticketID string) []*domain.TicketResource {
	var ms []TicketResourceModel
	if err := r.db.WithContext(ctx).Where("ticket_id = ?", ticketID).Order("created_at ASC, rowid ASC").Find(&ms).Error; err != nil {
		return nil
	}
	out := make([]*domain.TicketResource, len(ms))
	for i := range ms {
		out[i] = ms[i].ToDomain()
	}
	return out
}

func (r *TicketResourceRepository) SetTitle(id, title string) (*domain.TicketResource, error) {
	m := r.get(id)
	if m == nil {
		return nil, errResourceNotFound
	}
	m.Title, m.UpdatedAt = title, editedAt(m.CreatedAt)
	if err := r.db.Model(m).Updates(map[string]any{"title": m.Title, "updated_at": m.UpdatedAt}).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketResourceRepository) Delete(id string) error {
	res := r.db.Where("id = ?", id).Delete(&TicketResourceModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errResourceNotFound
	}
	return nil
}

func (r *TicketResourceRepository) DeleteByTicket(ticketID string) ([]string, error) {
	var ids []string
	if err := r.db.Model(&TicketResourceModel{}).Where("ticket_id = ?", ticketID).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, r.db.Where("ticket_id = ?", ticketID).Delete(&TicketResourceModel{}).Error
}

// TicketCommentRepository persists comments in SQLite via GORM.
type TicketCommentRepository struct{ db *gorm.DB }

func NewTicketCommentRepository(db *gorm.DB) *TicketCommentRepository {
	return &TicketCommentRepository{db: db}
}

var errCommentNotFound = &domain.StructuredError{Code: "COMMENT_NOT_FOUND", Message: "comment not found"}

func (r *TicketCommentRepository) Create(c *domain.TicketComment) (*domain.TicketComment, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	m := &TicketCommentModel{ID: id, TicketID: c.TicketID, ProjectID: c.ProjectID, Author: string(c.Author),
		SessionID: c.SessionID, AgentName: c.AgentName, Body: c.Body, CreatedAt: now, UpdatedAt: now}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketCommentRepository) get(id string) *TicketCommentModel {
	var ms []TicketCommentModel
	if err := r.db.Where("id = ?", id).Limit(1).Find(&ms).Error; err != nil || len(ms) == 0 {
		return nil
	}
	return &ms[0]
}

func (r *TicketCommentRepository) Get(id string) *domain.TicketComment {
	if m := r.get(id); m != nil {
		return m.ToDomain()
	}
	return nil
}

func (r *TicketCommentRepository) List(ctx context.Context, ticketID string, since time.Time) []*domain.TicketComment {
	q := r.db.WithContext(ctx).Where("ticket_id = ?", ticketID)
	if !since.IsZero() {
		q = q.Where("created_at > ?", since.UnixMilli())
	}
	var ms []TicketCommentModel
	if err := q.Order("created_at ASC, rowid ASC").Find(&ms).Error; err != nil {
		return nil
	}
	out := make([]*domain.TicketComment, len(ms))
	for i := range ms {
		out[i] = ms[i].ToDomain()
	}
	return out
}

func (r *TicketCommentRepository) SetBody(id, body string) (*domain.TicketComment, error) {
	m := r.get(id)
	if m == nil {
		return nil, errCommentNotFound
	}
	m.Body, m.UpdatedAt = body, editedAt(m.CreatedAt)
	if err := r.db.Model(m).Updates(map[string]any{"body": m.Body, "updated_at": m.UpdatedAt}).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketCommentRepository) Delete(id string) error {
	res := r.db.Where("id = ?", id).Delete(&TicketCommentModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errCommentNotFound
	}
	return nil
}

func (r *TicketCommentRepository) DeleteByTicket(ticketID string) error {
	return r.db.Where("ticket_id = ?", ticketID).Delete(&TicketCommentModel{}).Error
}

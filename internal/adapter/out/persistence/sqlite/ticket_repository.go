package sqlite

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

var _ ports.TicketRepository = (*TicketRepository)(nil)

// TicketRepository persists tickets in SQLite via GORM.
type TicketRepository struct {
	db *gorm.DB
}

// NewTicketRepository returns a new ticket repository.
func NewTicketRepository(db *gorm.DB) *TicketRepository {
	return &TicketRepository{db: db}
}

func (r *TicketRepository) Get(ctx context.Context, id string) *domain.Ticket {
	var m TicketModel
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *TicketRepository) ListByProject(ctx context.Context, projectID string) []*domain.Ticket {
	return r.list(r.db.WithContext(ctx).Where("project_id = ? AND archived_at IS NULL", projectID))
}

func (r *TicketRepository) ListArchivedByProject(ctx context.Context, projectID string) []*domain.Ticket {
	return r.list(r.db.WithContext(ctx).Where("project_id = ? AND archived_at IS NOT NULL", projectID).Order("archived_at DESC"))
}

func (r *TicketRepository) list(q *gorm.DB) []*domain.Ticket {
	var models []TicketModel
	if err := q.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Ticket, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *TicketRepository) Create(projectID, title, description string, status domain.TicketStatus, parentID string) (*domain.Ticket, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &TicketModel{ID: id, ProjectID: projectID, Title: title, Description: description, Status: string(status), ParentID: parentID}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketRepository) Update(id, title, description string, status domain.TicketStatus, parentID string) (*domain.Ticket, error) {
	var m TicketModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{"title": title, "description": description, "status": string(status), "parent_id": parentID}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketRepository) SetArchived(id string, at *time.Time) (*domain.Ticket, error) {
	var ms *int64
	if at != nil {
		v := at.UnixMilli()
		ms = &v
	}
	res := r.db.Model(&TicketModel{}).Where("id = ?", id).Update("archived_at", ms)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	var m TicketModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketRepository) Delete(id string) error {
	var m TicketModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		return err
	}
	// Remove this ticket's join rows; the documents themselves survive.
	if err := r.db.Where("ticket_id = ?", id).Delete(&TicketDocumentModel{}).Error; err != nil {
		return err
	}
	return r.db.Delete(&m).Error
}

// ActivityByProject answers each project's open and archived task counts
// and newest update, in one query.
func (r *TicketRepository) ActivityByProject() (map[string]ports.TaskActivity, error) {
	var rows []struct {
		ProjectID string
		Open      int
		Archived  int
		Last      int64
	}
	err := r.db.Model(&TicketModel{}).
		Select("project_id, SUM(CASE WHEN status <> ? THEN 1 ELSE 0 END) AS open, "+
			"SUM(CASE WHEN archived_at IS NOT NULL THEN 1 ELSE 0 END) AS archived, MAX(updated_at) AS last", string(domain.TicketStatusDone)).
		Group("project_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]ports.TaskActivity, len(rows))
	for _, row := range rows {
		out[row.ProjectID] = ports.TaskActivity{OpenCount: row.Open, ArchivedCount: row.Archived, LastUpdatedAt: time.UnixMilli(row.Last)}
	}
	return out, nil
}

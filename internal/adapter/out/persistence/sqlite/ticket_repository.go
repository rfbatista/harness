package sqlite

import (
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

func (r *TicketRepository) Get(id string) *domain.Ticket {
	var m TicketModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *TicketRepository) ListByProject(projectID string) []*domain.Ticket {
	var models []TicketModel
	if err := r.db.Where("project_id = ?", projectID).Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Ticket, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *TicketRepository) Create(projectID, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &TicketModel{ID: id, ProjectID: projectID, Title: title, Description: description, Status: string(status)}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *TicketRepository) Update(id, title, description string, status domain.TicketStatus) (*domain.Ticket, error) {
	var m TicketModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{"title": title, "description": description, "status": string(status)}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
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

// ActivityByProject answers each project's open task count and newest
// update, in one query.
func (r *TicketRepository) ActivityByProject() (map[string]ports.TaskActivity, error) {
	var rows []struct {
		ProjectID string
		Open      int
		Last      int64
	}
	err := r.db.Model(&TicketModel{}).
		Select("project_id, SUM(CASE WHEN status <> ? THEN 1 ELSE 0 END) AS open, MAX(updated_at) AS last", string(domain.TicketStatusDone)).
		Group("project_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]ports.TaskActivity, len(rows))
	for _, row := range rows {
		out[row.ProjectID] = ports.TaskActivity{OpenCount: row.Open, LastUpdatedAt: time.UnixMilli(row.Last)}
	}
	return out, nil
}

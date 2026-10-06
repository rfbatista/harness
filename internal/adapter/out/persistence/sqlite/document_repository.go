package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ ports.DocumentRepository = (*DocumentRepository)(nil)

// DocumentRepository persists documents and owns the ticket_documents join table.
type DocumentRepository struct {
	db *gorm.DB
}

// NewDocumentRepository returns a new document repository.
func NewDocumentRepository(db *gorm.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

func (r *DocumentRepository) Get(id string) *domain.Document {
	var m DocumentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *DocumentRepository) ListByProject(projectID string) []*domain.Document {
	var models []DocumentModel
	if err := r.db.Where("project_id = ?", projectID).Find(&models).Error; err != nil {
		return nil
	}
	return documentsToDomain(models)
}

func (r *DocumentRepository) ListByTicket(ticketID string) []*domain.Document {
	var models []DocumentModel
	err := r.db.
		Joins("JOIN ticket_documents ON ticket_documents.document_id = documents.id").
		Where("ticket_documents.ticket_id = ?", ticketID).
		Find(&models).Error
	if err != nil {
		return nil
	}
	return documentsToDomain(models)
}

func (r *DocumentRepository) Create(projectID, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := &DocumentModel{ID: id, ProjectID: projectID, Title: title, Format: string(format), Content: content}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

// Update replaces title and content; format replaces the stored format when
// set and keeps it when empty.
func (r *DocumentRepository) Update(id, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	var m DocumentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
		}
		return nil, err
	}
	updates := map[string]interface{}{"title": title, "content": content}
	if format != "" {
		updates["format"] = string(format)
	}
	if err := r.db.Model(&m).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *DocumentRepository) Delete(id string) error {
	var m DocumentModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "DOCUMENT_NOT_FOUND", Message: "document not found"}
		}
		return err
	}
	if err := r.db.Where("document_id = ?", id).Delete(&TicketDocumentModel{}).Error; err != nil {
		return err
	}
	return r.db.Delete(&m).Error
}

// Link creates a join row. It is idempotent: re-linking an existing pair is a no-op.
func (r *DocumentRepository) Link(ticketID, documentID string) error {
	m := &TicketDocumentModel{TicketID: ticketID, DocumentID: documentID}
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(m).Error
}

func (r *DocumentRepository) Unlink(ticketID, documentID string) error {
	return r.db.
		Where("ticket_id = ? AND document_id = ?", ticketID, documentID).
		Delete(&TicketDocumentModel{}).Error
}

func documentsToDomain(models []DocumentModel) []*domain.Document {
	out := make([]*domain.Document, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

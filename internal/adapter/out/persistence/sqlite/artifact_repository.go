package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ArtifactRepository = (*ArtifactRepository)(nil)

// ArtifactRepository persists artifacts in SQLite via GORM.
type ArtifactRepository struct{ db *gorm.DB }

func NewArtifactRepository(db *gorm.DB) *ArtifactRepository { return &ArtifactRepository{db: db} }

func (r *ArtifactRepository) Create(a *domain.Artifact) (*domain.Artifact, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	scope := a.Scope
	if scope == "" {
		scope = domain.ArtifactScopeTask
	}
	m := &ArtifactModel{
		ID: id, SessionID: a.SessionID, Target: artifactTarget(a.Path, a.URL), TicketID: a.TicketID, ProjectID: a.ProjectID,
		Kind: string(a.Kind), Title: a.Title, Note: a.Note, Path: a.Path, URL: a.URL, Mime: a.Mime, SizeBytes: a.SizeBytes, Revision: a.Revision,
		Scope: string(scope), Snapshot: a.Snapshot,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return r.withLinks(m.ToDomain())[0], nil
}

func (r *ArtifactRepository) Update(a *domain.Artifact) error {
	updated := a.UpdatedAt
	if updated.IsZero() {
		updated = time.Now()
	}
	res := r.db.Model(&ArtifactModel{}).Where("id = ?", a.ID).Updates(map[string]any{
		"title": a.Title, "note": a.Note, "kind": string(a.Kind), "mime": a.Mime,
		"size_bytes": a.SizeBytes, "revision": a.Revision, "snapshot": a.Snapshot, "updated_at": updated.UnixMilli(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
	}
	return nil
}

func (r *ArtifactRepository) Get(id string) *domain.Artifact {
	var m ArtifactModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return r.withLinks(m.ToDomain())[0]
}

func (r *ArtifactRepository) FindByTarget(sessionID, path, url string) *domain.Artifact {
	var m ArtifactModel
	if err := r.db.First(&m, "session_id = ? AND target = ?", sessionID, artifactTarget(path, url)).Error; err != nil {
		return nil
	}
	return r.withLinks(m.ToDomain())[0]
}

func (r *ArtifactRepository) List(f ports.ArtifactFilter) []*domain.Artifact {
	q := r.db.Model(&ArtifactModel{})
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.TicketID != "" {
		// What the task produced, and the project artifacts attached to it.
		q = q.Where("ticket_id = ? OR id IN (?)", f.TicketID,
			r.db.Model(&ArtifactTicketModel{}).Select("artifact_id").Where("ticket_id = ?", f.TicketID))
	}
	if f.ProjectID != "" {
		q = q.Where("project_id = ?", f.ProjectID)
	}
	switch f.Scope {
	case "":
	case domain.ArtifactScopeTask:
		// Rows with no scope count too: they predate it.
		q = q.Where("scope = ? OR scope = '' OR scope IS NULL", string(f.Scope))
	default:
		q = q.Where("scope = ?", string(f.Scope))
	}
	var models []ArtifactModel
	if err := q.Order("updated_at DESC, id ASC").Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Artifact, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return r.withLinks(out...)
}

// Delete removes the record and its links to tasks.
func (r *ArtifactRepository) Delete(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&ArtifactModel{}, "id = ?", id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
		}
		return tx.Delete(&ArtifactTicketModel{}, "artifact_id = ?", id).Error
	})
}

// SetScope moves an artifact between task and project scope and records
// whether the harness holds a copy of its bytes. It bumps updated_at, so
// listings and watchers notice the move. Moving to task drops its links to
// tasks: only a project artifact is attached anywhere.
func (r *ArtifactRepository) SetScope(id string, scope domain.ArtifactScope, snapshot bool) (*domain.Artifact, error) {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&ArtifactModel{}).Where("id = ?", id).Updates(map[string]any{
			"scope": string(scope), "snapshot": snapshot, "updated_at": time.Now().UnixMilli(),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
		}
		if scope == domain.ArtifactScopeProject {
			return nil
		}
		return tx.Delete(&ArtifactTicketModel{}, "artifact_id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	return r.Get(id), nil
}

// DeleteTaskScopedBySession removes a session's task-scoped records (the
// session is being deleted) and returns their ids. Project ones stay.
func (r *ArtifactRepository) DeleteTaskScopedBySession(sessionID string) ([]string, error) {
	var ids []string
	err := r.db.Transaction(func(tx *gorm.DB) error {
		q := tx.Model(&ArtifactModel{}).Where("session_id = ?", sessionID).
			Where("scope = ? OR scope = '' OR scope IS NULL", string(domain.ArtifactScopeTask))
		if err := q.Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Delete(&ArtifactTicketModel{}, "artifact_id IN ?", ids).Error; err != nil {
			return err
		}
		return tx.Delete(&ArtifactModel{}, "id IN ?", ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// Attach links an artifact to a task; changed is false when it already was.
func (r *ArtifactRepository) Attach(artifactID, ticketID string) (bool, error) {
	res := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&ArtifactTicketModel{ArtifactID: artifactID, TicketID: ticketID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Detach unlinks an artifact from a task; changed is false when it was not linked.
func (r *ArtifactRepository) Detach(artifactID, ticketID string) (bool, error) {
	res := r.db.Delete(&ArtifactTicketModel{}, "artifact_id = ? AND ticket_id = ?", artifactID, ticketID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// DetachTicket unlinks every artifact from a task (the task was deleted) and
// returns the ids of those that were linked.
func (r *ArtifactRepository) DetachTicket(ticketID string) ([]string, error) {
	ids := []string{}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ArtifactTicketModel{}).Where("ticket_id = ?", ticketID).Order("artifact_id").Pluck("artifact_id", &ids).Error; err != nil {
			return err
		}
		return tx.Delete(&ArtifactTicketModel{}, "ticket_id = ?", ticketID).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// withLinks fills each artifact's AttachedTicketIDs, oldest link first. A
// link whose task row is gone is skipped: the task's deletion is still being
// handled, or raced an attach.
func (r *ArtifactRepository) withLinks(list ...*domain.Artifact) []*domain.Artifact {
	ids := make([]string, 0, len(list))
	byID := make(map[string]*domain.Artifact, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		a.AttachedTicketIDs = []string{}
		ids = append(ids, a.ID)
		byID[a.ID] = a
	}
	if len(ids) == 0 {
		return list
	}
	var links []ArtifactTicketModel
	r.db.Model(&ArtifactTicketModel{}).
		Joins("JOIN tickets ON tickets.id = artifact_tickets.ticket_id").
		Where("artifact_tickets.artifact_id IN ?", ids).
		Order("artifact_tickets.created_at ASC, artifact_tickets.ticket_id ASC").
		Select("artifact_tickets.artifact_id, artifact_tickets.ticket_id").
		Find(&links)
	for _, l := range links {
		if a := byID[l.ArtifactID]; a != nil {
			a.AttachedTicketIDs = append(a.AttachedTicketIDs, l.TicketID)
		}
	}
	return list
}

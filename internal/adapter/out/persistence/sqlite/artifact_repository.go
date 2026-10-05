package sqlite

import (
	"time"

	"gorm.io/gorm"

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
	m := &ArtifactModel{
		ID: id, SessionID: a.SessionID, Target: artifactTarget(a.Path, a.URL), TicketID: a.TicketID, ProjectID: a.ProjectID,
		Kind: string(a.Kind), Title: a.Title, Note: a.Note, Path: a.Path, URL: a.URL, Mime: a.Mime, SizeBytes: a.SizeBytes, Revision: a.Revision,
	}
	if err := r.db.Create(m).Error; err != nil {
		return nil, err
	}
	return m.ToDomain(), nil
}

func (r *ArtifactRepository) Update(a *domain.Artifact) error {
	updated := a.UpdatedAt
	if updated.IsZero() {
		updated = time.Now()
	}
	res := r.db.Model(&ArtifactModel{}).Where("id = ?", a.ID).Updates(map[string]any{
		"title": a.Title, "note": a.Note, "kind": string(a.Kind), "mime": a.Mime,
		"size_bytes": a.SizeBytes, "revision": a.Revision, "updated_at": updated.UnixMilli(),
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
	return m.ToDomain()
}

func (r *ArtifactRepository) FindByTarget(sessionID, path, url string) *domain.Artifact {
	var m ArtifactModel
	if err := r.db.First(&m, "session_id = ? AND target = ?", sessionID, artifactTarget(path, url)).Error; err != nil {
		return nil
	}
	return m.ToDomain()
}

func (r *ArtifactRepository) List(f ports.ArtifactFilter) []*domain.Artifact {
	q := r.db.Model(&ArtifactModel{})
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.TicketID != "" {
		q = q.Where("ticket_id = ?", f.TicketID)
	}
	var models []ArtifactModel
	if err := q.Order("updated_at DESC, id ASC").Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Artifact, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

func (r *ArtifactRepository) Delete(id string) error {
	res := r.db.Delete(&ArtifactModel{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
	}
	return nil
}

func (r *ArtifactRepository) DeleteBySession(sessionID string) error {
	return r.db.Delete(&ArtifactModel{}, "session_id = ?", sessionID).Error
}

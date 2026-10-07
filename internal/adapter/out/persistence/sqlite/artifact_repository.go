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
	return m.ToDomain(), nil
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

// SetScope moves an artifact between task and project scope and records
// whether the harness holds a copy of its bytes. It bumps updated_at, so
// listings and watchers notice the move.
func (r *ArtifactRepository) SetScope(id string, scope domain.ArtifactScope, snapshot bool) (*domain.Artifact, error) {
	res := r.db.Model(&ArtifactModel{}).Where("id = ?", id).Updates(map[string]any{
		"scope": string(scope), "snapshot": snapshot, "updated_at": time.Now().UnixMilli(),
	})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
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
		return tx.Delete(&ArtifactModel{}, "id IN ?", ids).Error
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

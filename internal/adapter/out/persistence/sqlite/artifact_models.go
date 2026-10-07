package sqlite

import (
	"time"

	"operators-mcp/internal/domain"
)

// ArtifactModel is the GORM model for domain.Artifact. Target is "path:<path>"
// or "url:<url>", so the (session, target) unique index holds the identity
// rule for both kinds without colliding on empty paths.
type ArtifactModel struct {
	ID        string `gorm:"primaryKey"`
	SessionID string `gorm:"column:session_id;index;uniqueIndex:idx_artifacts_session_target"`
	Target    string `gorm:"column:target;uniqueIndex:idx_artifacts_session_target"`
	TicketID  string `gorm:"column:ticket_id;index"`
	ProjectID string `gorm:"column:project_id;index"`
	Kind      string `gorm:"column:kind"`
	Title     string `gorm:"column:title"`
	Note      string `gorm:"column:note"`
	Path      string `gorm:"column:path"`
	URL       string `gorm:"column:url"`
	Mime      string `gorm:"column:mime"`
	SizeBytes int64  `gorm:"column:size_bytes"`
	Revision  int    `gorm:"column:revision"`
	// Scope is task or project. The column default covers rows written
	// before it existed; ToDomain covers an empty value either way.
	Scope     string `gorm:"column:scope;default:task;index"`
	Snapshot  bool   `gorm:"column:snapshot;default:false"`
	CreatedAt int64  `gorm:"autoCreateTime:milli"`
	UpdatedAt int64  `gorm:"autoUpdateTime:milli"`
}

func (ArtifactModel) TableName() string { return "artifacts" }

// ArtifactTicketModel links a project artifact to a task it is attached to
// (never the task that produced it). CreatedAt orders an artifact's links.
type ArtifactTicketModel struct {
	ArtifactID string `gorm:"column:artifact_id;primaryKey;index"`
	TicketID   string `gorm:"column:ticket_id;primaryKey;index"`
	CreatedAt  int64  `gorm:"autoCreateTime:nano"`
}

func (ArtifactTicketModel) TableName() string { return "artifact_tickets" }

func artifactTarget(path, url string) string {
	if url != "" {
		return "url:" + url
	}
	return "path:" + path
}

func (m *ArtifactModel) ToDomain() *domain.Artifact {
	if m == nil {
		return nil
	}
	scope := domain.ArtifactScope(m.Scope)
	if scope == "" {
		scope = domain.ArtifactScopeTask
	}
	return &domain.Artifact{
		ID: m.ID, SessionID: m.SessionID, TicketID: m.TicketID, ProjectID: m.ProjectID,
		Kind: domain.ArtifactKind(m.Kind), Title: m.Title, Note: m.Note, Path: m.Path, URL: m.URL,
		Mime: m.Mime, SizeBytes: m.SizeBytes, Revision: m.Revision,
		Scope: scope, Snapshot: m.Snapshot,
		CreatedAt: time.UnixMilli(m.CreatedAt), UpdatedAt: time.UnixMilli(m.UpdatedAt),
	}
}

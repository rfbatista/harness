package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.PullRequestRepository = (*PullRequestRepository)(nil)

// PullRequestModel is the GORM model for domain.PullRequest. A pull request
// is identified by its repository and number; it holds no foreign key to its
// workspace, so the record outlives the workspace row.
type PullRequestModel struct {
	ID           string `gorm:"primaryKey"`
	ProjectID    string `gorm:"column:project_id;index"`
	RepositoryID string `gorm:"column:repository_id;uniqueIndex:idx_pull_requests_repo_number"`
	Number       int    `gorm:"uniqueIndex:idx_pull_requests_repo_number"`
	WorkspaceID  string `gorm:"column:workspace_id;index"`
	URL          string
	Title        string
	HeadBranch   string `gorm:"column:head_branch"`
	BaseBranch   string `gorm:"column:base_branch"`
	Draft        bool
	State        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TableName overrides the table name.
func (PullRequestModel) TableName() string { return "pull_requests" }

func (m *PullRequestModel) toDomain() *domain.PullRequest {
	return &domain.PullRequest{
		WorkspaceID: m.WorkspaceID, RepositoryID: m.RepositoryID, ProjectID: m.ProjectID,
		Number: m.Number, URL: m.URL, Title: m.Title, HeadBranch: m.HeadBranch, BaseBranch: m.BaseBranch,
		Draft: m.Draft, State: domain.PullRequestState(m.State), CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

// PullRequestRepository persists workspaces' pull requests in SQLite.
type PullRequestRepository struct {
	db *gorm.DB
}

// NewPullRequestRepository returns the repository.
func NewPullRequestRepository(db *gorm.DB) *PullRequestRepository {
	return &PullRequestRepository{db: db}
}

// Save inserts the pull request, or updates the row of the same repository
// and number; created_at and the row id stay as first stored.
func (r *PullRequestRepository) Save(pr *domain.PullRequest) (*domain.PullRequest, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	created := pr.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	m := &PullRequestModel{
		ID: id, ProjectID: pr.ProjectID, RepositoryID: pr.RepositoryID, Number: pr.Number,
		WorkspaceID: pr.WorkspaceID, URL: pr.URL, Title: pr.Title, HeadBranch: pr.HeadBranch,
		BaseBranch: pr.BaseBranch, Draft: pr.Draft, State: string(pr.State),
		CreatedAt: created, UpdatedAt: time.Now(),
	}
	err = r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "repository_id"}, {Name: "number"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"project_id", "workspace_id", "url", "title", "head_branch", "base_branch", "draft", "state", "updated_at",
		}),
	}).Create(m).Error
	if err != nil {
		return nil, err
	}
	var stored PullRequestModel
	if err := r.db.Where("repository_id = ? AND number = ?", pr.RepositoryID, pr.Number).Take(&stored).Error; err != nil {
		return nil, err
	}
	return stored.toDomain(), nil
}

// LatestForWorkspace is the workspace's newest pull request, or nil.
func (r *PullRequestRepository) LatestForWorkspace(workspaceID string) *domain.PullRequest {
	var ms []PullRequestModel
	if err := r.db.Where("workspace_id = ?", workspaceID).Order("created_at DESC, number DESC").Limit(1).Find(&ms).Error; err != nil || len(ms) == 0 {
		return nil
	}
	return ms[0].toDomain()
}

// DeleteByProject removes every pull request record of the project.
func (r *PullRequestRepository) DeleteByProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&PullRequestModel{}).Error
}

package sqlite

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchRequestStore = (*BranchRequestRepository)(nil)

// BranchRequestModel is the GORM model of domain.BranchRequest.
type BranchRequestModel struct {
	ID             string `gorm:"primaryKey"`
	SessionID      string `gorm:"column:session_id;index"`
	TaskID         string `gorm:"column:task_id;index"`
	RepositoryID   string `gorm:"column:repository_id"`
	SessionBranch  string `gorm:"column:session_branch"`
	Action         string
	Branch         string
	NewName        string `gorm:"column:new_name"`
	StartKind      string `gorm:"column:start_kind"`
	StartRef       string `gorm:"column:start_ref"`
	StartCommit    string `gorm:"column:start_commit"`
	RemoteName     string `gorm:"column:remote_name"`
	RemoteURL      string `gorm:"column:remote_url"`
	RemoteNewName  string `gorm:"column:remote_new_name"`
	RemoteBranch   string `gorm:"column:remote_branch"`
	Commits        *int
	CommitsAgainst string `gorm:"column:commits_against"`
	Commit         string `gorm:"column:commit_sha"`
	Command        string
	Via            string
	Reason         string
	State          string `gorm:"index"`
	Note           string
	RequestedAt    time.Time  `gorm:"column:requested_at"`
	AnsweredAt     *time.Time `gorm:"column:answered_at"`
}

// TableName overrides the table name.
func (BranchRequestModel) TableName() string { return "branch_requests" }

// BranchRequestRepository persists branch requests in SQLite via GORM.
type BranchRequestRepository struct{ db *gorm.DB }

// NewBranchRequestRepository returns a new branch request repository.
func NewBranchRequestRepository(db *gorm.DB) *BranchRequestRepository {
	return &BranchRequestRepository{db: db}
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (m BranchRequestModel) domain() domain.BranchRequest {
	r := domain.BranchRequest{
		ID: m.ID, SessionID: m.SessionID, TaskID: nilIfEmpty(m.TaskID), RepositoryID: m.RepositoryID, SessionBranch: m.SessionBranch,
		Action: domain.BranchActionKind(m.Action), Branch: m.Branch, NewName: nilIfEmpty(m.NewName),
		RemoteBranch: nilIfEmpty(m.RemoteBranch), Commits: m.Commits, CommitsAgainst: nilIfEmpty(m.CommitsAgainst), Commit: m.Commit, Command: m.Command, Via: m.Via, Reason: m.Reason,
		State: domain.BranchRequestState(m.State), Note: m.Note, RequestedAt: m.RequestedAt, AnsweredAt: m.AnsweredAt,
	}
	if m.StartKind != "" {
		r.StartPoint = &domain.BranchStart{Kind: m.StartKind, Ref: m.StartRef, Commit: m.StartCommit}
	}
	if m.RemoteName != "" || m.RemoteURL != "" {
		r.Remote = &domain.BranchRemote{Name: m.RemoteName, URL: m.RemoteURL, NewName: nilIfEmpty(m.RemoteNewName)}
	}
	return r
}

func branchRequestModel(r domain.BranchRequest) BranchRequestModel {
	m := BranchRequestModel{
		ID: r.ID, SessionID: r.SessionID, RepositoryID: r.RepositoryID, SessionBranch: r.SessionBranch,
		Action: string(r.Action), Branch: r.Branch, Commits: r.Commits, CommitsAgainst: derefOr(r.CommitsAgainst), Commit: r.Commit, Command: r.Command, Via: r.Via, Reason: r.Reason,
		State: string(r.State), Note: r.Note, RequestedAt: r.RequestedAt, AnsweredAt: r.AnsweredAt,
	}
	if r.TaskID != nil {
		m.TaskID = *r.TaskID
	}
	if r.NewName != nil {
		m.NewName = *r.NewName
	}
	if r.RemoteBranch != nil {
		m.RemoteBranch = *r.RemoteBranch
	}
	if s := r.StartPoint; s != nil {
		m.StartKind, m.StartRef, m.StartCommit = s.Kind, s.Ref, s.Commit
	}
	if rm := r.Remote; rm != nil {
		m.RemoteName, m.RemoteURL = rm.Name, rm.URL
		if rm.NewName != nil {
			m.RemoteNewName = *rm.NewName
		}
	}
	return m
}

// Add implements ports.BranchRequestStore; it sets the request's id.
func (r *BranchRequestRepository) Add(ctx context.Context, req *domain.BranchRequest) error {
	id, err := genID()
	if err != nil {
		return err
	}
	req.ID = id
	m := branchRequestModel(*req)
	return r.db.WithContext(ctx).Create(&m).Error
}

// Get implements ports.BranchRequestStore; nil when there is none.
func (r *BranchRequestRepository) Get(ctx context.Context, id string) (*domain.BranchRequest, error) {
	var m BranchRequestModel
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	req := m.domain()
	return &req, nil
}

// Save implements ports.BranchRequestStore.
func (r *BranchRequestRepository) Save(ctx context.Context, req *domain.BranchRequest) error {
	return r.db.WithContext(ctx).Model(&BranchRequestModel{}).Where("id = ?", req.ID).
		Updates(map[string]any{"state": string(req.State), "note": req.Note, "answered_at": req.AnsweredAt}).Error
}

// List implements ports.BranchRequestStore.
func (r *BranchRequestRepository) List(ctx context.Context, f domain.BranchRequestFilter) ([]domain.BranchRequest, error) {
	q := r.db.WithContext(ctx)
	if f.SessionID != "" {
		q = q.Where("session_id = ?", f.SessionID)
	}
	if f.RepositoryID != "" {
		q = q.Where("repository_id = ?", f.RepositoryID)
	}
	if f.TaskID != "" {
		q = q.Where("task_id = ?", f.TaskID)
	}
	if f.State != "" {
		q = q.Where("state = ?", string(f.State))
	}
	var rows []BranchRequestModel
	if err := q.Order("requested_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BranchRequest, len(rows))
	for i, m := range rows {
		out[i] = m.domain()
	}
	return out, nil
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BranchWatchStore = (*BranchWatchRepository)(nil)

// BranchBaselineModel is a stored snapshot of a repository's branches.
type BranchBaselineModel struct {
	Key     string `gorm:"primaryKey"`
	Data    string
	TakenAt time.Time `gorm:"column:taken_at"`
}

// TableName overrides the table name.
func (BranchBaselineModel) TableName() string { return "branch_baselines" }

// BranchStatModel is one branch of a session counted against its base.
type BranchStatModel struct {
	SessionID string `gorm:"primaryKey;column:session_id"`
	Branch    string `gorm:"primaryKey"`
	BaseRef   string `gorm:"column:base_ref"`
	Ahead     int
	Behind    int
}

// TableName overrides the table name.
func (BranchStatModel) TableName() string { return "branch_stats" }

// BranchFindingModel is a branch or push no request explained.
type BranchFindingModel struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"column:repository_id;uniqueIndex:branch_findings_key"`
	SessionID    string `gorm:"column:session_id;index"`
	Branch       string `gorm:"uniqueIndex:branch_findings_key"`
	Kind         string `gorm:"uniqueIndex:branch_findings_key"`
	Remote       string `gorm:"uniqueIndex:branch_findings_key"`
	RemoteBranch string `gorm:"column:remote_branch;uniqueIndex:branch_findings_key"`
	Commit       string `gorm:"uniqueIndex:branch_findings_key"`
	NewName      string `gorm:"column:new_name"`
	Candidates   string
	FoundAt      time.Time `gorm:"column:found_at"`
}

// TableName overrides the table name.
func (BranchFindingModel) TableName() string { return "branch_findings" }

// BranchWatchRepository persists the Branches list's stored state.
type BranchWatchRepository struct{ db *gorm.DB }

// NewBranchWatchRepository returns the repository.
func NewBranchWatchRepository(db *gorm.DB) *BranchWatchRepository {
	return &BranchWatchRepository{db: db}
}

// Baseline implements ports.BranchWatchStore.
func (r *BranchWatchRepository) Baseline(ctx context.Context, key string) (*ports.BranchBaseline, error) {
	var m BranchBaselineModel
	err := r.db.WithContext(ctx).Where("key = ?", key).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b ports.BranchBaseline
	if err := json.Unmarshal([]byte(m.Data), &b); err != nil {
		return nil, err
	}
	b.TakenAt = m.TakenAt
	return &b, nil
}

// SaveBaseline implements ports.BranchWatchStore.
func (r *BranchWatchRepository) SaveBaseline(ctx context.Context, key string, b ports.BranchBaseline) error {
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&BranchBaselineModel{Key: key, Data: string(data), TakenAt: b.TakenAt}).Error
}

// Stats implements ports.BranchWatchStore.
func (r *BranchWatchRepository) Stats(ctx context.Context, sessionIDs ...string) ([]ports.BranchStat, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []BranchStatModel
	if err := r.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ports.BranchStat, len(rows))
	for i, m := range rows {
		out[i] = ports.BranchStat{SessionID: m.SessionID, Branch: m.Branch, BaseRef: m.BaseRef, Ahead: m.Ahead, Behind: m.Behind}
	}
	return out, nil
}

// SaveStats implements ports.BranchWatchStore.
func (r *BranchWatchRepository) SaveStats(ctx context.Context, sessionID string, stats []ports.BranchStat) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", sessionID).Delete(&BranchStatModel{}).Error; err != nil {
			return err
		}
		for _, s := range stats {
			m := BranchStatModel{SessionID: sessionID, Branch: s.Branch, BaseRef: s.BaseRef, Ahead: s.Ahead, Behind: s.Behind}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// AddFinding implements ports.BranchWatchStore.
func (r *BranchWatchRepository) AddFinding(ctx context.Context, f *ports.BranchFinding) (bool, error) {
	id, err := genID()
	if err != nil {
		return false, err
	}
	m := BranchFindingModel{
		ID: id, RepositoryID: f.RepositoryID, SessionID: f.SessionID, Branch: f.Branch, Kind: string(f.Kind),
		NewName: f.NewName, Remote: f.Remote, RemoteBranch: f.RemoteBranch, Commit: f.Commit,
		Candidates: strings.Join(f.Candidates, ","), FoundAt: f.FoundAt,
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&m)
	if res.Error != nil {
		return false, res.Error
	}
	f.ID = id
	return res.RowsAffected == 1, nil
}

// Findings implements ports.BranchWatchStore.
func (r *BranchWatchRepository) Findings(ctx context.Context, sessionIDs []string) ([]ports.BranchFinding, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	var rows []BranchFindingModel
	if err := r.db.WithContext(ctx).Where("session_id IN ?", sessionIDs).Order("found_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ports.BranchFinding, len(rows))
	for i, m := range rows {
		f := ports.BranchFinding{
			ID: m.ID, RepositoryID: m.RepositoryID, SessionID: m.SessionID, Branch: m.Branch, Kind: domain.BranchFindingKind(m.Kind),
			NewName: m.NewName, Remote: m.Remote, RemoteBranch: m.RemoteBranch, Commit: m.Commit, FoundAt: m.FoundAt,
		}
		if m.Candidates != "" {
			f.Candidates = strings.Split(m.Candidates, ",")
		}
		out[i] = f
	}
	return out, nil
}

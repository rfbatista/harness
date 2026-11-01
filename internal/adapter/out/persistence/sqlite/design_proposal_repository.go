package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignProposalStore = (*DesignProposalRepository)(nil)

// DesignProposalModel is the GORM model of a design session's draft and its
// proposal: the draft record as JSON.
type DesignProposalModel struct {
	ID             string `gorm:"primaryKey"`
	DesignSystemID string `gorm:"column:design_system_id;index"`
	SessionID      string `gorm:"column:session_id;index"`
	State          string
	BaseVersion    int `gorm:"column:base_version"`
	Summary        string
	DraftJSON      string `gorm:"column:draft_json"`
	DecidedVersion int    `gorm:"column:decided_version"`
	DecisionsJSON  string `gorm:"column:decisions_json"`
	Approved       int
	Rejected       int
	SubmittedAt    time.Time `gorm:"column:submitted_at"`
	DecidedAt      time.Time `gorm:"column:decided_at"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName overrides the table name.
func (DesignProposalModel) TableName() string { return "design_system_proposals" }

func proposalModel(p *domain.DesignProposal) (*DesignProposalModel, error) {
	draft, err := json.Marshal(p.Draft)
	if err != nil {
		return nil, err
	}
	decisions, err := json.Marshal(p.Decisions)
	if err != nil {
		return nil, err
	}
	return &DesignProposalModel{DecisionsJSON: string(decisions),
		ID: p.ID, DesignSystemID: p.DesignSystemID, SessionID: p.SessionID, State: p.State, BaseVersion: p.BaseVersion,
		Summary: p.Summary, DraftJSON: string(draft), DecidedVersion: p.DecidedVersion, Approved: p.Approved, Rejected: p.Rejected,
		SubmittedAt: p.SubmittedAt, DecidedAt: p.DecidedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}, nil
}

func (m *DesignProposalModel) toDomain() (*domain.DesignProposal, error) {
	p := &domain.DesignProposal{
		ID: m.ID, DesignSystemID: m.DesignSystemID, SessionID: m.SessionID, State: m.State, BaseVersion: m.BaseVersion,
		Summary: m.Summary, DecidedVersion: m.DecidedVersion, Approved: m.Approved, Rejected: m.Rejected,
		SubmittedAt: m.SubmittedAt, DecidedAt: m.DecidedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if err := json.Unmarshal([]byte(m.DraftJSON), &p.Draft); err != nil {
		return nil, err
	}
	if m.DecisionsJSON != "" {
		if err := json.Unmarshal([]byte(m.DecisionsJSON), &p.Decisions); err != nil {
			return nil, err
		}
	}
	p.Draft.Normalize()
	return p, nil
}

// DesignProposalRepository persists design sessions' drafts and proposals.
type DesignProposalRepository struct {
	db *gorm.DB
}

// NewDesignProposalRepository returns a new proposal store.
func NewDesignProposalRepository(db *gorm.DB) *DesignProposalRepository {
	return &DesignProposalRepository{db: db}
}

// Create implements ports.DesignProposalStore.
func (r *DesignProposalRepository) Create(ctx context.Context, p *domain.DesignProposal) error {
	id, err := genID()
	if err != nil {
		return err
	}
	p.ID, p.CreatedAt, p.UpdatedAt = id, time.Now(), time.Now()
	m, err := proposalModel(p)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// Update implements ports.DesignProposalStore.
func (r *DesignProposalRepository) Update(ctx context.Context, p *domain.DesignProposal) error {
	p.UpdatedAt = time.Now()
	m, err := proposalModel(p)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Save(m).Error
}

// Get implements ports.DesignProposalStore. Find, not First: a miss is not an
// error worth logging.
func (r *DesignProposalRepository) Get(ctx context.Context, id string) (*domain.DesignProposal, error) {
	return r.one(r.db.WithContext(ctx).Where("id = ?", id))
}

// ActiveBySession implements ports.DesignProposalStore.
func (r *DesignProposalRepository) ActiveBySession(ctx context.Context, sessionID string) (*domain.DesignProposal, error) {
	return r.one(r.db.WithContext(ctx).Where("session_id = ? AND state IN ?", sessionID, []string{domain.ProposalDrafting, domain.ProposalOpen}).Order("created_at DESC"))
}

func (r *DesignProposalRepository) one(q *gorm.DB) (*domain.DesignProposal, error) {
	var rows []DesignProposalModel
	if err := q.Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].toDomain()
}

// ListByDesignSystem implements ports.DesignProposalStore.
func (r *DesignProposalRepository) ListByDesignSystem(ctx context.Context, designSystemID string) ([]*domain.DesignProposal, error) {
	var rows []DesignProposalModel
	if err := r.db.WithContext(ctx).Where("design_system_id = ?", designSystemID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.DesignProposal, 0, len(rows))
	for i := range rows {
		p, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// DeleteByDesignSystem implements ports.DesignProposalStore.
func (r *DesignProposalRepository) DeleteByDesignSystem(ctx context.Context, designSystemID string) error {
	return r.db.WithContext(ctx).Where("design_system_id = ?", designSystemID).Delete(&DesignProposalModel{}).Error
}

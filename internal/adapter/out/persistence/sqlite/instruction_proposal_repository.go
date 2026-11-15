package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.InstructionProposalStore = (*InstructionProposalRepository)(nil)

// InstructionProposalModel is the GORM model of an instruction-file proposal;
// its files and skills are JSON.
type InstructionProposalModel struct {
	ID           string `gorm:"primaryKey"`
	RepositoryID string `gorm:"column:repository_id;index"`
	ProjectID    string `gorm:"column:project_id;index"`
	TaskID       string `gorm:"column:task_id;index"`
	SessionID    string `gorm:"column:session_id"`
	Summary      string
	SkillsJSON   string `gorm:"column:skills_json"`
	FilesJSON    string `gorm:"column:files_json"`
	State        string
	Note         string
	CreatedAt    time.Time
	DecidedAt    *time.Time
}

// TableName overrides the table name.
func (InstructionProposalModel) TableName() string { return "instruction_proposals" }

type instructionProposalFileJSON struct {
	Path        string `json:"path"`
	Op          string `json:"op"`
	BaseVersion int    `json:"base_version"`
	BaseContent string `json:"base_content"`
	Content     string `json:"content"`
}

func instructionProposalModel(p *domain.InstructionProposal) (*InstructionProposalModel, error) {
	skills, err := json.Marshal(p.Skills)
	if err != nil {
		return nil, err
	}
	files := make([]instructionProposalFileJSON, len(p.Files))
	for i, f := range p.Files {
		files[i] = instructionProposalFileJSON(f)
	}
	fj, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}
	return &InstructionProposalModel{ID: p.ID, RepositoryID: p.RepositoryID, ProjectID: p.ProjectID, TaskID: p.TaskID, SessionID: p.SessionID,
		Summary: p.Summary, SkillsJSON: string(skills), FilesJSON: string(fj), State: p.State, Note: p.Note, CreatedAt: p.CreatedAt, DecidedAt: p.DecidedAt}, nil
}

func (m *InstructionProposalModel) toDomain() (*domain.InstructionProposal, error) {
	p := &domain.InstructionProposal{ID: m.ID, RepositoryID: m.RepositoryID, ProjectID: m.ProjectID, TaskID: m.TaskID, SessionID: m.SessionID,
		Summary: m.Summary, State: m.State, Note: m.Note, CreatedAt: m.CreatedAt, DecidedAt: m.DecidedAt}
	if err := json.Unmarshal([]byte(m.SkillsJSON), &p.Skills); err != nil {
		return nil, err
	}
	var files []instructionProposalFileJSON
	if err := json.Unmarshal([]byte(m.FilesJSON), &files); err != nil {
		return nil, err
	}
	for _, f := range files {
		p.Files = append(p.Files, domain.InstructionProposalFile(f))
	}
	return p, nil
}

// InstructionProposalRepository persists instruction-file proposals.
type InstructionProposalRepository struct{ db *gorm.DB }

// NewInstructionProposalRepository returns a new proposal store.
func NewInstructionProposalRepository(db *gorm.DB) *InstructionProposalRepository {
	return &InstructionProposalRepository{db: db}
}

// CreateInstructionProposal implements ports.InstructionProposalStore.
func (r *InstructionProposalRepository) CreateInstructionProposal(ctx context.Context, p *domain.InstructionProposal) error {
	m, err := instructionProposalModel(p)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(m).Error
}

// GetInstructionProposal implements ports.InstructionProposalStore.
func (r *InstructionProposalRepository) GetInstructionProposal(ctx context.Context, id string) (*domain.InstructionProposal, error) {
	var rows []InstructionProposalModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].toDomain()
}

func (r *InstructionProposalRepository) list(ctx context.Context, column, value string) ([]*domain.InstructionProposal, error) {
	var rows []InstructionProposalModel
	if err := r.db.WithContext(ctx).Where(column+" = ?", value).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.InstructionProposal, 0, len(rows))
	for i := range rows {
		p, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ListInstructionProposalsByRepository implements ports.InstructionProposalStore.
func (r *InstructionProposalRepository) ListInstructionProposalsByRepository(ctx context.Context, repositoryID string) ([]*domain.InstructionProposal, error) {
	return r.list(ctx, "repository_id", repositoryID)
}

// ListInstructionProposalsByTask implements ports.InstructionProposalStore.
func (r *InstructionProposalRepository) ListInstructionProposalsByTask(ctx context.Context, taskID string) ([]*domain.InstructionProposal, error) {
	return r.list(ctx, "task_id", taskID)
}

// UpdateInstructionProposal implements ports.InstructionProposalStore.
func (r *InstructionProposalRepository) UpdateInstructionProposal(ctx context.Context, p *domain.InstructionProposal) error {
	return dbFor(ctx, r.db).Model(&InstructionProposalModel{}).Where("id = ?", p.ID).
		Updates(map[string]any{"state": p.State, "note": p.Note, "decided_at": p.DecidedAt}).Error
}

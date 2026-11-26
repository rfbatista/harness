package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchCaseStore = (*BenchCaseRepository)(nil)

// BenchCaseModel is one bench case of a project's eval bench (ADR 0015). The
// mock tools and the expectation are stored as JSON: they are read and written
// whole.
type BenchCaseModel struct {
	ID        string `gorm:"primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Name      string
	Prompt    string
	Tools     string
	Expect    string
	Source    string
	CreatedAt time.Time
}

// TableName overrides the table name.
func (BenchCaseModel) TableName() string { return "bench_cases" }

// BenchCaseRepository stores the bench cases.
type BenchCaseRepository struct{ db *gorm.DB }

// NewBenchCaseRepository returns the store.
func NewBenchCaseRepository(db *gorm.DB) *BenchCaseRepository {
	return &BenchCaseRepository{db: db}
}

// List answers the project's cases, oldest first.
func (r *BenchCaseRepository) List(ctx context.Context, projectID string) ([]domain.BenchCase, error) {
	var rows []BenchCaseModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.BenchCase, 0, len(rows))
	for _, row := range rows {
		c, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Save stores a case.
func (r *BenchCaseRepository) Save(ctx context.Context, c domain.BenchCase) error {
	tools, err := json.Marshal(c.Tools)
	if err != nil {
		return err
	}
	expect, err := json.Marshal(c.Expect)
	if err != nil {
		return err
	}
	row := BenchCaseModel{ID: c.ID, ProjectID: c.ProjectID, Name: c.Name, Prompt: c.Prompt,
		Tools: string(tools), Expect: string(expect), Source: string(c.Source), CreatedAt: c.CreatedAt}
	return r.db.WithContext(ctx).Save(&row).Error
}

// Delete removes a case of the project and reports whether there was one.
func (r *BenchCaseRepository) Delete(ctx context.Context, projectID, caseID string) (bool, error) {
	res := r.db.WithContext(ctx).Where("project_id = ? AND id = ?", projectID, caseID).Delete(&BenchCaseModel{})
	return res.RowsAffected > 0, res.Error
}

func (m BenchCaseModel) toDomain() (domain.BenchCase, error) {
	c := domain.BenchCase{ID: m.ID, ProjectID: m.ProjectID, Name: m.Name, Prompt: m.Prompt,
		Source: domain.BenchCaseSource(m.Source), CreatedAt: m.CreatedAt}
	if err := json.Unmarshal([]byte(m.Tools), &c.Tools); err != nil {
		return c, err
	}
	if c.Tools == nil {
		c.Tools = []domain.BenchTool{}
	}
	return c, json.Unmarshal([]byte(m.Expect), &c.Expect)
}

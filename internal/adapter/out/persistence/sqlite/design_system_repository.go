package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.DesignSystemStore = (*DesignSystemRepository)(nil)

// DesignSystemModel is the GORM model of a design system's identity. The slug
// and the name (as NameKey, which ignores case) are unique in the project.
type DesignSystemModel struct {
	ID          string `gorm:"primaryKey"`
	ProjectID   string `gorm:"column:project_id;uniqueIndex:idx_design_systems_slug,priority:1;uniqueIndex:idx_design_systems_name,priority:1"`
	Name        string
	NameKey     string `gorm:"column:name_key;uniqueIndex:idx_design_systems_name,priority:2"`
	Slug        string `gorm:"uniqueIndex:idx_design_systems_slug,priority:2"`
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TableName overrides the table name.
func (DesignSystemModel) TableName() string { return "design_systems" }

func (m *DesignSystemModel) toDomain() *domain.DesignSystemIdentity {
	return &domain.DesignSystemIdentity{
		ID: m.ID, ProjectID: m.ProjectID, Name: m.Name, Slug: m.Slug, Description: m.Description,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func nameKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// DesignSystemVersionModel is the GORM model of one design-system version:
// the whole record as JSON, and its summary. Rows are only ever created.
type DesignSystemVersionModel struct {
	DesignSystemID string `gorm:"primaryKey;column:design_system_id"`
	Version        int    `gorm:"primaryKey;autoIncrement:false"`
	At             time.Time
	By             string
	Source         string
	ProposalID     string `gorm:"column:proposal_id"`
	Message        string
	SummaryJSON    string `gorm:"column:summary_json"`
	RecordJSON     string `gorm:"column:record_json"`
}

// TableName overrides the table name.
func (DesignSystemVersionModel) TableName() string { return "design_system_versions" }

// dropProjectKeyedDesignSystemVersions drops the versions table of the one
// design system per project (ADR 0054), keyed by project_id. Nothing of it was
// ever merged, so there is nothing to carry over.
func dropProjectKeyedDesignSystemVersions(db *gorm.DB) error {
	m := db.Migrator()
	if m.HasTable(&DesignSystemVersionModel{}) && !m.HasColumn(&DesignSystemVersionModel{}, "design_system_id") {
		return m.DropTable(&DesignSystemVersionModel{})
	}
	return nil
}

func (m *DesignSystemVersionModel) toDomain() (*domain.DesignSystemVersion, error) {
	v := &domain.DesignSystemVersion{
		DesignSystemID: m.DesignSystemID, Version: m.Version, At: m.At, By: m.By, Source: m.Source,
		ProposalID: m.ProposalID, Message: m.Message,
	}
	if err := json.Unmarshal([]byte(m.SummaryJSON), &v.Summary); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(m.RecordJSON), &v.Record); err != nil {
		return nil, err
	}
	v.Record.Normalize()
	return v, nil
}

// DesignSystemRepository persists design systems and their versions in SQLite
// via GORM.
type DesignSystemRepository struct {
	db *gorm.DB
}

// NewDesignSystemRepository returns a new design system store.
func NewDesignSystemRepository(db *gorm.DB) *DesignSystemRepository {
	return &DesignSystemRepository{db: db}
}

// slugTaken is SLUG_TAKEN naming the field that clashes.
func slugTaken(field string) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "SLUG_TAKEN", Message: "the project already has a design system with that " + field},
		Details:         map[string]any{"field": field},
	}
}

// clash says which field of the project's other design systems d repeats
// ("" when none); exceptID is d itself on an update.
func clash(tx *gorm.DB, d *domain.DesignSystemIdentity, exceptID string) (string, error) {
	var rows []DesignSystemModel
	if err := tx.Where("project_id = ? AND id <> ?", d.ProjectID, exceptID).Find(&rows).Error; err != nil {
		return "", err
	}
	for _, r := range rows {
		if exceptID == "" && r.Slug == d.Slug {
			return "slug", nil
		}
		if r.NameKey == nameKey(d.Name) {
			return "name", nil
		}
	}
	return "", nil
}

// Create implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Create(ctx context.Context, d *domain.DesignSystemIdentity) error {
	id, err := genID()
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if field, err := clash(tx, d, ""); err != nil || field != "" {
			if err != nil {
				return err
			}
			return slugTaken(field)
		}
		now := time.Now()
		m := DesignSystemModel{
			ID: id, ProjectID: d.ProjectID, Name: d.Name, NameKey: nameKey(d.Name), Slug: d.Slug,
			Description: d.Description, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		*d = *m.toDomain()
		return nil
	})
}

// Update implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Update(ctx context.Context, d *domain.DesignSystemIdentity) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []DesignSystemModel
		if err := tx.Where("id = ?", d.ID).Limit(1).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return &domain.StructuredError{Code: "DESIGN_SYSTEM_NOT_FOUND", Message: "design system not found"}
		}
		d.ProjectID = rows[0].ProjectID
		if field, err := clash(tx, d, d.ID); err != nil || field != "" {
			if err != nil {
				return err
			}
			return slugTaken(field)
		}
		rows[0].Name, rows[0].NameKey, rows[0].Description, rows[0].UpdatedAt = d.Name, nameKey(d.Name), d.Description, time.Now()
		if err := tx.Save(&rows[0]).Error; err != nil {
			return err
		}
		*d = *rows[0].toDomain()
		return nil
	})
}

// Identity implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Identity(ctx context.Context, designSystemID string) (*domain.DesignSystemIdentity, error) {
	var rows []DesignSystemModel
	if err := r.db.WithContext(ctx).Where("id = ?", designSystemID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].toDomain(), nil
}

// ListByProject implements ports.DesignSystemStore.
func (r *DesignSystemRepository) ListByProject(ctx context.Context, projectID string) ([]*domain.DesignSystemIdentity, error) {
	var rows []DesignSystemModel
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("created_at ASC, rowid ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.DesignSystemIdentity, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].toDomain())
	}
	return out, nil
}

// Delete implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Delete(ctx context.Context, designSystemID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("design_system_id = ?", designSystemID).Delete(&DesignSystemVersionModel{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", designSystemID).Delete(&DesignSystemModel{}).Error
	})
}

// DeleteByProject implements ports.DesignSystemStore.
func (r *DesignSystemRepository) DeleteByProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ids := tx.Model(&DesignSystemModel{}).Select("id").Where("project_id = ?", projectID)
		if err := tx.Where("design_system_id IN (?)", ids).Delete(&DesignSystemVersionModel{}).Error; err != nil {
			return err
		}
		return tx.Where("project_id = ?", projectID).Delete(&DesignSystemModel{}).Error
	})
}

// latestVersion reads the design system's newest row; nil when it has none. Find,
// not First: a miss is not an error worth logging.
func latestVersion(db *gorm.DB, designSystemID string) (*DesignSystemVersionModel, error) {
	var rows []DesignSystemVersionModel
	if err := db.Where("design_system_id = ?", designSystemID).Order("version DESC").Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// Latest implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Latest(ctx context.Context, designSystemID string) (*domain.DesignSystemVersion, error) {
	m, err := latestVersion(r.db.WithContext(ctx), designSystemID)
	if err != nil || m == nil {
		return nil, err
	}
	return m.toDomain()
}

// Get implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Get(ctx context.Context, designSystemID string, version int) (*domain.DesignSystemVersion, error) {
	var rows []DesignSystemVersionModel
	if err := r.db.WithContext(ctx).Where("design_system_id = ? AND version = ?", designSystemID, version).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0].toDomain()
}

// List implements ports.DesignSystemStore.
func (r *DesignSystemRepository) List(ctx context.Context, designSystemID string, limit, before int) ([]*domain.DesignSystemVersion, error) {
	q := r.db.WithContext(ctx).Where("design_system_id = ?", designSystemID)
	if before > 0 {
		q = q.Where("version < ?", before)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []DesignSystemVersionModel
	if err := q.Order("version DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.DesignSystemVersion, 0, len(rows))
	for i := range rows {
		v, err := rows[i].toDomain()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Append implements ports.DesignSystemStore.
func (r *DesignSystemRepository) Append(ctx context.Context, v *domain.DesignSystemVersion, baseVersion int) error {
	record, err := json.Marshal(v.Record)
	if err != nil {
		return err
	}
	summary, err := json.Marshal(v.Summary)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		cur, err := latestVersion(tx, v.DesignSystemID)
		if err != nil {
			return err
		}
		stored := 0
		if cur != nil {
			stored = cur.Version
		}
		if stored != baseVersion {
			return &domain.DetailedError{
				StructuredError: &domain.StructuredError{Code: "DESIGN_SYSTEM_CHANGED", Message: "the design system was saved since you loaded it"},
				Details:         map[string]any{"current_version": stored},
			}
		}
		m := DesignSystemVersionModel{
			DesignSystemID: v.DesignSystemID, Version: stored + 1, At: v.At, By: v.By, Source: v.Source, ProposalID: v.ProposalID,
			Message: v.Message, SummaryJSON: string(summary), RecordJSON: string(record),
		}
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		v.Version = m.Version
		return nil
	})
}

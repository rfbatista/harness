package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TokenSaverActivationRepository = (*ActivationRepository)(nil)

// ActivationModel is one token saver's activation at one level
// (domain.Activation). Inherit is never stored.
type ActivationModel struct {
	Saver     string `gorm:"primaryKey"`
	Level     string `gorm:"primaryKey"`
	OwnerID   string `gorm:"column:owner_id;primaryKey"`
	ProjectID string `gorm:"column:project_id;index"`
	Value     string
	UpdatedAt time.Time
}

// TableName overrides the table name.
func (ActivationModel) TableName() string { return "token_saver_activations" }

// ActivationRepository stores token saver activations.
type ActivationRepository struct{ db *gorm.DB }

// NewActivationRepository returns the store.
func NewActivationRepository(db *gorm.DB) *ActivationRepository {
	return &ActivationRepository{db: db}
}

// List returns owner's stored activations.
func (r *ActivationRepository) List(owner domain.ActivationOwner) ([]domain.Activation, error) {
	var models []ActivationModel
	if err := r.db.Where("level = ? AND owner_id = ?", owner.Level, owner.ID).Order("saver").Find(&models).Error; err != nil {
		return nil, err
	}
	return activations(models), nil
}

// ListSaver returns saver's stored activations, at every level.
func (r *ActivationRepository) ListSaver(saver domain.TokenSaver) ([]domain.Activation, error) {
	var models []ActivationModel
	if err := r.db.Where("saver = ?", saver).Order("level, owner_id").Find(&models).Error; err != nil {
		return nil, err
	}
	return activations(models), nil
}

func activations(models []ActivationModel) []domain.Activation {
	out := make([]domain.Activation, 0, len(models))
	for _, m := range models {
		out = append(out, domain.Activation{
			Saver:     domain.TokenSaver(m.Saver),
			Owner:     domain.ActivationOwner{Level: domain.ActivationLevel(m.Level), ID: m.OwnerID},
			ProjectID: m.ProjectID,
			Value:     domain.ActivationValue(m.Value),
		})
	}
	return out
}

// Put creates or replaces a's activation.
func (r *ActivationRepository) Put(a domain.Activation) error {
	m := ActivationModel{
		Saver: string(a.Saver), Level: string(a.Owner.Level), OwnerID: a.Owner.ID,
		ProjectID: a.ProjectID, Value: string(a.Value), UpdatedAt: time.Now(),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "saver"}, {Name: "level"}, {Name: "owner_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"project_id", "value", "updated_at"}),
	}).Create(&m).Error
}

// Delete removes one saver's activation for owner, if stored.
func (r *ActivationRepository) Delete(saver domain.TokenSaver, owner domain.ActivationOwner) error {
	return r.db.Where("saver = ? AND level = ? AND owner_id = ?", saver, owner.Level, owner.ID).Delete(&ActivationModel{}).Error
}

// DeleteOwner removes every activation of owner.
func (r *ActivationRepository) DeleteOwner(owner domain.ActivationOwner) error {
	return r.db.Where("level = ? AND owner_id = ?", owner.Level, owner.ID).Delete(&ActivationModel{}).Error
}

// DeleteProject removes every activation in the project, at every level.
func (r *ActivationRepository) DeleteProject(projectID string) error {
	return r.db.Where("project_id = ?", projectID).Delete(&ActivationModel{}).Error
}

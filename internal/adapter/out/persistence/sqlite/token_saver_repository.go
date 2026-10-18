package sqlite

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.TokenSaverInstallationRepository = (*TokenSaverInstallationRepository)(nil)

// TokenSaverInstallationModel is one installed token saver
// (domain.TokenSaverInstallation).
type TokenSaverInstallationModel struct {
	Saver       string `gorm:"primaryKey"`
	Version     string
	Dir         string
	Path        string
	InstalledAt time.Time
}

// TableName overrides the table name.
func (TokenSaverInstallationModel) TableName() string { return "token_saver_installations" }

// TokenSaverInstallationRepository stores the installed token savers.
type TokenSaverInstallationRepository struct{ db *gorm.DB }

// NewTokenSaverInstallationRepository returns the store.
func NewTokenSaverInstallationRepository(db *gorm.DB) *TokenSaverInstallationRepository {
	return &TokenSaverInstallationRepository{db: db}
}

// List returns every installation by saver.
func (r *TokenSaverInstallationRepository) List() ([]*domain.TokenSaverInstallation, error) {
	var models []TokenSaverInstallationModel
	if err := r.db.Order("saver").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.TokenSaverInstallation, 0, len(models))
	for _, m := range models {
		out = append(out, &domain.TokenSaverInstallation{
			Saver: domain.TokenSaver(m.Saver), Version: m.Version, Dir: m.Dir, Path: m.Path, InstalledAt: m.InstalledAt,
		})
	}
	return out, nil
}

// Put creates or replaces the saver's installation.
func (r *TokenSaverInstallationRepository) Put(in *domain.TokenSaverInstallation) error {
	m := TokenSaverInstallationModel{Saver: string(in.Saver), Version: in.Version, Dir: in.Dir, Path: in.Path, InstalledAt: in.InstalledAt}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "saver"}},
		DoUpdates: clause.AssignmentColumns([]string{"version", "dir", "path", "installed_at"}),
	}).Create(&m).Error
}

// Delete removes the saver's installation, if any.
func (r *TokenSaverInstallationRepository) Delete(saver domain.TokenSaver) error {
	return r.db.Where("saver = ?", string(saver)).Delete(&TokenSaverInstallationModel{}).Error
}

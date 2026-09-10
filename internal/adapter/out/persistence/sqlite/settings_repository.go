package sqlite

import (
	"errors"

	"operators-mcp/internal/application/ports"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ ports.SettingsRepository = (*SettingsRepository)(nil)

// SettingsRepository persists global key/value settings in SQLite via GORM.
type SettingsRepository struct {
	db *gorm.DB
}

// NewSettingsRepository returns a new settings repository.
func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) All() (map[string]string, error) {
	var models []SettingModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(models))
	for _, m := range models {
		out[m.Key] = m.Value
	}
	return out, nil
}

func (r *SettingsRepository) Get(key string) (string, error) {
	var m SettingModel
	err := r.db.First(&m, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return m.Value, nil
}

func (r *SettingsRepository) Set(key, value string) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&SettingModel{Key: key, Value: value}).Error
}

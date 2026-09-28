package sqlite

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"gorm.io/gorm"
)

var _ ports.SkillRepository = (*SkillRepository)(nil)

// SkillRepository persists skills in SQLite via GORM.
type SkillRepository struct {
	db *gorm.DB
}

// NewSkillRepository returns a new skill repository.
func NewSkillRepository(db *gorm.DB) *SkillRepository {
	return &SkillRepository{db: db}
}

func (r *SkillRepository) loadFiles(skillID string) []domain.SkillFile {
	var models []SkillFileModel
	if err := r.db.Where("skill_id = ?", skillID).Order("path").Find(&models).Error; err != nil {
		return nil
	}
	if len(models) == 0 {
		return nil
	}
	out := make([]domain.SkillFile, 0, len(models))
	for i := range models {
		out = append(out, models[i].ToDomain())
	}
	return out
}

// replaceFiles rewrites all file rows for a skill inside tx.
func replaceFiles(tx *gorm.DB, skillID string, files []domain.SkillFile) error {
	if err := tx.Where("skill_id = ?", skillID).Delete(&SkillFileModel{}).Error; err != nil {
		return err
	}
	for _, f := range domain.NormalizeSkillFiles(files) {
		id, err := genID()
		if err != nil {
			return err
		}
		if err := tx.Create(skillFileModel(id, skillID, f)).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *SkillRepository) hydrate(m *SkillModel) *domain.Skill {
	skill := m.ToDomain()
	skill.Files = r.loadFiles(m.ID)
	return skill
}

func (r *SkillRepository) Get(id string) *domain.Skill {
	var m SkillModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		return nil
	}
	return r.hydrate(&m)
}

func (r *SkillRepository) List() []*domain.Skill {
	var models []SkillModel
	if err := r.db.Find(&models).Error; err != nil {
		return nil
	}
	out := make([]*domain.Skill, 0, len(models))
	for i := range models {
		out = append(out, r.hydrate(&models[i]))
	}
	return out
}

func (r *SkillRepository) Create(input domain.SkillInput) (*domain.Skill, error) {
	id, err := genID()
	if err != nil {
		return nil, err
	}
	m := skillModelFromInput(id, input)
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		return replaceFiles(tx, id, input.Files)
	})
	if err != nil {
		return nil, err
	}
	return r.Get(id), nil
}

func (r *SkillRepository) Update(id string, input domain.SkillInput) (*domain.Skill, error) {
	var m SkillModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
		}
		return nil, err
	}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"name":          input.Name,
			"description":   input.Description,
			"content":       input.Content,
			"path":          input.Path,
			"license":       input.License,
			"compatibility": input.Compatibility,
			"metadata":      stringMap(input.Metadata),
			"allowed_tools": input.AllowedTools,
		}
		if err := tx.Model(&m).Updates(updates).Error; err != nil {
			return err
		}
		return replaceFiles(tx, id, input.Files)
	})
	if err != nil {
		return nil, err
	}
	return r.Get(id), nil
}

func (r *SkillRepository) Delete(id string) error {
	var m SkillModel
	if err := r.db.First(&m, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
		}
		return err
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("skill_id = ?", id).Delete(&SkillFileModel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&m).Error
	})
}

func (r *SkillRepository) exists(id string) bool {
	var count int64
	r.db.Model(&SkillModel{}).Where("id = ?", id).Count(&count)
	return count > 0
}

func (r *SkillRepository) ListFiles(skillID string) ([]domain.SkillFile, error) {
	if !r.exists(skillID) {
		return nil, &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
	}
	return r.loadFiles(skillID), nil
}

func (r *SkillRepository) PutFile(skillID string, f domain.SkillFile) error {
	if !r.exists(skillID) {
		return &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
	}
	norm, ok := domain.NormalizeSkillPath(f.Path)
	if !ok {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "invalid skill file path"}
	}
	f.Path = norm
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("skill_id = ? AND path = ?", skillID, norm).Delete(&SkillFileModel{}).Error; err != nil {
			return err
		}
		id, err := genID()
		if err != nil {
			return err
		}
		return tx.Create(skillFileModel(id, skillID, f)).Error
	})
}

func (r *SkillRepository) RenameFile(skillID, oldPath, newPath string) error {
	if !r.exists(skillID) {
		return &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
	}
	from, ok1 := domain.NormalizeSkillPath(oldPath)
	to, ok2 := domain.NormalizeSkillPath(newPath)
	if !ok1 || !ok2 {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "invalid skill file path"}
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var models []SkillFileModel
		if err := tx.Where("skill_id = ?", skillID).Find(&models).Error; err != nil {
			return err
		}
		matched := false
		for i := range models {
			m := &models[i]
			newRel, ok := domain.RenamedSkillPath(m.Path, from, to)
			if !ok {
				continue
			}
			matched = true
			if err := tx.Model(&SkillFileModel{}).
				Where("id = ?", m.ID).
				Update("path", newRel).Error; err != nil {
				return err
			}
		}
		if !matched {
			return &domain.StructuredError{Code: "SKILL_FILE_NOT_FOUND", Message: "skill file not found"}
		}
		return nil
	})
}

func (r *SkillRepository) DeleteFile(skillID, path string) error {
	if !r.exists(skillID) {
		return &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
	}
	target, ok := domain.NormalizeSkillPath(path)
	if !ok {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "invalid skill file path"}
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Where("skill_id = ? AND (path = ? OR path LIKE ?)",
			skillID, target, target+"/%").Delete(&SkillFileModel{}).Error
	})
}

func (r *SkillRepository) SetPublishState(skillID string, state domain.SkillPublishState) error {
	if !r.exists(skillID) {
		return &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill not found"}
	}
	return r.db.Model(&SkillModel{}).Where("id = ?", skillID).Updates(map[string]interface{}{
		"published_slug": state.Slug,
		"published_at":   state.At,
		"publish_error":  state.Error,
	}).Error
}

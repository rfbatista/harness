package capabilities

import "operators-mcp/internal/domain"

// The skill use cases live in harnesskit/skill; this file delegates to them.

// ListSkills returns all skills, hydrated.
func (s *Service) ListSkills() []*domain.Skill { return s.skills.List() }

// GetSkill returns one skill by id with hydration, or nil if not found.
func (s *Service) GetSkill(id string) *domain.Skill { return s.skills.Get(id) }

// ValidateSkillPath checks a filesystem skill path without persisting.
func (s *Service) ValidateSkillPath(path string) (*domain.SkillPathValidation, error) {
	return s.skills.ValidatePath(path)
}

// InspectSkill returns full on-disk skill details for a path.
func (s *Service) InspectSkill(path string) (*domain.SkillInspection, error) {
	return s.skills.Inspect(path)
}

// CreateSkill creates a skill from a directory tree, inline content, or a path.
func (s *Service) CreateSkill(input domain.SkillInput) (*domain.Skill, error) {
	return s.skills.Create(input)
}

// UpdateSkill updates a skill's metadata and/or tree, resyncing any published copy.
func (s *Service) UpdateSkill(id string, input domain.SkillInput) (*domain.Skill, error) {
	return s.skills.Update(id, input)
}

// PutSkillFile creates or replaces one file/folder in a skill's tree.
func (s *Service) PutSkillFile(id string, f domain.SkillFile) (*domain.Skill, error) {
	return s.skills.PutFile(id, f)
}

// RenameSkillFile moves a file or folder within a skill's tree.
func (s *Service) RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error) {
	return s.skills.RenameFile(id, oldPath, newPath)
}

// DeleteSkillFile removes a file or folder from a skill's tree.
func (s *Service) DeleteSkillFile(id, path string) (*domain.Skill, error) {
	return s.skills.DeleteFile(id, path)
}

// ListSkillFiles returns a skill's stored tree.
func (s *Service) ListSkillFiles(id string) ([]domain.SkillFile, error) {
	return s.skills.ListFiles(id)
}

// ImportSkillFromPath creates a skill from a directory tree on disk.
func (s *Service) ImportSkillFromPath(path string) (*domain.Skill, error) {
	return s.skills.ImportFromPath(path)
}

// DeleteSkill deletes a skill and unpublishes it. SkillDeleted is announced
// first, so references to it go before it does.
func (s *Service) DeleteSkill(id string) error { return s.skills.Delete(id) }

// PublishSkill mirrors a skill's tree to the configured publish root.
func (s *Service) PublishSkill(id string, force bool) (*domain.Skill, error) {
	return s.skills.Publish(id, force)
}

// UnpublishSkill removes a skill's published folder and stops syncing it.
func (s *Service) UnpublishSkill(id string) (*domain.Skill, error) {
	return s.skills.Unpublish(id)
}

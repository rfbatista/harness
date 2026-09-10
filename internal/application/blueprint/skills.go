package blueprint

import "operators-mcp/internal/domain"

// The skill use cases live in harnesskit/skill; this file delegates to them and
// supplies the two things the library deliberately does not know about: where
// this application keeps its publish root, and what else references a skill.

// ListSkills returns all skills, hydrated.
func (s *Service) ListSkills() []*domain.Skill { return s.skillSvc.List() }

// GetSkill returns one skill by id with hydration, or nil if not found.
func (s *Service) GetSkill(id string) *domain.Skill { return s.skillSvc.Get(id) }

// hydrateSkill fills derived fields on a stored record. Agent resolution needs
// it for skills it loaded through its own join.
func (s *Service) hydrateSkill(sk *domain.Skill) *domain.Skill { return s.skillSvc.Hydrate(sk) }

// ValidateSkillPath checks a filesystem skill path without persisting.
func (s *Service) ValidateSkillPath(path string) (*domain.SkillPathValidation, error) {
	return s.skillSvc.ValidatePath(path)
}

// InspectSkill returns full on-disk skill details for a path.
func (s *Service) InspectSkill(path string) (*domain.SkillInspection, error) {
	return s.skillSvc.Inspect(path)
}

// CreateSkill creates a skill from a directory tree, inline content, or a path.
func (s *Service) CreateSkill(input domain.SkillInput) (*domain.Skill, error) {
	return s.skillSvc.Create(input)
}

// UpdateSkill updates a skill's metadata and/or tree, resyncing any published copy.
func (s *Service) UpdateSkill(id string, input domain.SkillInput) (*domain.Skill, error) {
	return s.skillSvc.Update(id, input)
}

// PutSkillFile creates or replaces one file/folder in a skill's tree.
func (s *Service) PutSkillFile(id string, f domain.SkillFile) (*domain.Skill, error) {
	return s.skillSvc.PutFile(id, f)
}

// RenameSkillFile moves a file or folder within a skill's tree.
func (s *Service) RenameSkillFile(id, oldPath, newPath string) (*domain.Skill, error) {
	return s.skillSvc.RenameFile(id, oldPath, newPath)
}

// DeleteSkillFile removes a file or folder from a skill's tree.
func (s *Service) DeleteSkillFile(id, path string) (*domain.Skill, error) {
	return s.skillSvc.DeleteFile(id, path)
}

// ListSkillFiles returns a skill's stored tree.
func (s *Service) ListSkillFiles(id string) ([]domain.SkillFile, error) {
	return s.skillSvc.ListFiles(id)
}

// ImportSkillFromPath creates a skill from a directory tree on disk.
func (s *Service) ImportSkillFromPath(path string) (*domain.Skill, error) {
	return s.skillSvc.ImportFromPath(path)
}

// DeleteSkill deletes a skill, unpublishing it and unlinking it from agents.
func (s *Service) DeleteSkill(id string) error { return s.skillSvc.Delete(id) }

// PublishSkill mirrors a skill's tree to the configured publish root.
func (s *Service) PublishSkill(id string, force bool) (*domain.Skill, error) {
	return s.skillSvc.Publish(id, force)
}

// UnpublishSkill removes a skill's published folder and stops syncing it.
func (s *Service) UnpublishSkill(id string) (*domain.Skill, error) {
	return s.skillSvc.Unpublish(id)
}

// unlinkSkillFromAgents removes a skill from every agent that references it.
// It backs skill.Hooks.BeforeDeleteRow: the library owns skill deletion, but
// only this application knows that agents point at skills.
func (s *Service) unlinkSkillFromAgents(sk *domain.Skill) error {
	if s.Agents == nil || sk == nil {
		return nil
	}
	for _, a := range s.Agents.List() {
		if !containsString(a.SkillIDs, sk.ID) {
			continue
		}
		filtered := make([]string, 0, len(a.SkillIDs))
		for _, skillID := range a.SkillIDs {
			if skillID != sk.ID {
				filtered = append(filtered, skillID)
			}
		}
		if _, err := s.Agents.Update(a.ID, a.Name, a.Description, a.PromptID, filtered, a.MCPServerIDs); err != nil {
			return err
		}
	}
	return nil
}

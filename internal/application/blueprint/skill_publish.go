package blueprint

import (
	"path/filepath"

	"operators-mcp/internal/domain"
)

// Publishing itself lives in harnesskit/skill. What stays here is the settings
// layer it reads through: where the publish root comes from, and what happens
// when a user changes it.

// publishRoot returns the configured publish root with "~" expanded, or "" when
// publishing is not configured. It backs skill.Service's RootFunc, so it is
// read on every operation rather than captured once.
func (s *Service) publishRoot() string {
	if s.Settings == nil {
		return ""
	}
	root, err := s.Settings.Get(domain.SettingSkillsPublishRoot)
	if err != nil {
		return ""
	}
	return domain.ExpandUserPath(root)
}

// GetSettings returns all global settings.
func (s *Service) GetSettings() (map[string]string, error) {
	if s.Settings == nil {
		return map[string]string{}, nil
	}
	all, err := s.Settings.All()
	if err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]string{}
	}
	return all, nil
}

// UpdateSettings merges the given keys into the stored settings. Changing the
// skills publish root republishes every published skill under the new root and
// removes their folders from the old one.
func (s *Service) UpdateSettings(values map[string]string) (map[string]string, error) {
	if s.Settings == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "settings are not available"}
	}
	oldRoot := s.publishRoot()
	rawRoot, changingRoot := values[domain.SettingSkillsPublishRoot]
	newRoot := domain.ExpandUserPath(rawRoot)
	if changingRoot && newRoot != "" && !filepath.IsAbs(newRoot) {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "publish root must be an absolute path"}
	}
	for key, value := range values {
		if err := s.Settings.Set(key, value); err != nil {
			return nil, err
		}
	}
	if changingRoot && newRoot != oldRoot {
		s.skillSvc.MigratePublishRoot(oldRoot, newRoot)
	}
	return s.GetSettings()
}

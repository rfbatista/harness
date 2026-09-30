package domain

import "path/filepath"

// SettingSkillsPublishRoot is the settings key holding the directory that
// published skills are written to. Empty means publishing is not configured.
const SettingSkillsPublishRoot = "skills.publish_root"

// SettingWorkspacesRoot is the settings key holding the directory that session
// worktrees are created under. Empty means the built-in default.
const SettingWorkspacesRoot = "workspaces.root"

// ValidateSetting checks a setting's value before it is stored. Keys without
// rules accept anything.
func ValidateSetting(key, value string) error {
	switch key {
	case SettingSkillsPublishRoot:
		if root := ExpandUserPath(value); root != "" && !filepath.IsAbs(root) {
			return &StructuredError{Code: "INVALID_INPUT", Message: "publish root must be an absolute path"}
		}
	}
	return nil
}

package domain

import (
	"path/filepath"
	"strings"
)

// SettingSkillsPublishRoot is the settings key holding the directory that
// published skills are written to. Empty means publishing is not configured.
const SettingSkillsPublishRoot = "skills.publish_root"

// SettingWorkspacesRoot is the settings key holding the directory that session
// worktrees are created under. Empty means the built-in default.
const SettingWorkspacesRoot = "workspaces.root"

// SettingAgentsMoveDocuments is the settings key behind "Agents move
// documents to project": "true" lets a session move its task's documents to
// project level; unset or "false", only the person does (ADR 0020).
const SettingAgentsMoveDocuments = "documents.agents_move_to_project"

// AgentsMoveDocuments reports whether the stored value of
// SettingAgentsMoveDocuments allows agent sessions to move documents.
func AgentsMoveDocuments(value string) bool { return value == "true" }

// ValidateSetting checks a setting's value before it is stored. Keys without
// rules accept anything.
func ValidateSetting(key, value string) error {
	switch key {
	case SettingSkillsPublishRoot:
		if root := ExpandUserPath(value); root != "" && !filepath.IsAbs(root) {
			return &StructuredError{Code: "INVALID_INPUT", Message: "publish root must be an absolute path"}
		}
	case SettingCavemanIntensity:
		if _, err := ParseCavemanIntensity(value); err != nil {
			return err
		}
	case SettingAgentsMoveDocuments:
		if value != "" && value != "true" && value != "false" {
			return &StructuredError{Code: "INVALID_INPUT", Message: key + " must be true or false"}
		}
	case SettingModelArchitect, SettingModelImplementation:
		if err := validateModelSetting(key, value); err != nil {
			return err
		}
	}
	return validateProviderSetting(key, value)
}

// A notification delivery provider is configured by two settings keyed by its
// name: notifications.<name>.enabled (true or false) and
// notifications.<name>.level (the lowest level it delivers). Unset, the
// provider's own defaults apply.
const notificationSettingPrefix = "notifications."

// NotificationProviderEnabledKey is the setting that turns a provider on or off.
func NotificationProviderEnabledKey(name string) string {
	return notificationSettingPrefix + name + ".enabled"
}

// NotificationProviderLevelKey is the setting holding the lowest level a
// provider delivers.
func NotificationProviderLevelKey(name string) string {
	return notificationSettingPrefix + name + ".level"
}

func validateProviderSetting(key, value string) error {
	if !strings.HasPrefix(key, notificationSettingPrefix) {
		return nil
	}
	switch {
	case strings.HasSuffix(key, ".enabled"):
		if value != "" && value != "true" && value != "false" {
			return &StructuredError{Code: "INVALID_INPUT", Message: key + " must be true or false"}
		}
	case strings.HasSuffix(key, ".level"):
		if value != "" {
			if _, err := ParseNotificationLevel(value); err != nil {
				return &StructuredError{Code: "INVALID_INPUT", Message: key + " must be info or action"}
			}
		}
	}
	return nil
}

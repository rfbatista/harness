package domain

import "slices"

// A session runs on one of two model tiers. An architect session plans the
// task, so it gets the architect model; a session another session started
// (through the start_task_session tool) carries out part of that plan, so it
// gets the cheaper implementation model. A session a person starts outside
// architect mode gets neither: the CLI picks its model.
const (
	// SettingModelArchitect is the settings key holding the model of
	// architect-mode sessions.
	SettingModelArchitect = "models.claude.architect"
	// SettingModelImplementation is the settings key holding the model of
	// sessions started by another session.
	SettingModelImplementation = "models.claude.implementation"
)

// ModelDefault is the model setting value that leaves the choice to the CLI:
// the session starts with no --model. An unset setting is not the same: it
// takes the tier's built-in default.
const ModelDefault = "default"

// The built-in tier models, used while their setting is unset.
const (
	DefaultArchitectModel      = "opus"
	DefaultImplementationModel = "sonnet"
)

// ModelChoices are the values a model setting accepts, in the order a person
// is offered them. The names are the CLI's aliases, which follow the newest
// model of each family.
var ModelChoices = []string{ModelDefault, "fable", "opus", "sonnet", "haiku"}

func validateModelSetting(key, value string) error {
	if value == "" {
		return nil
	}
	for _, c := range ModelChoices {
		if value == c {
			return nil
		}
	}
	return &StructuredError{Code: "INVALID_INPUT", Message: key + ` must be one of "default", "fable", "opus", "sonnet" or "haiku"`}
}

// TierModel reads a tier's stored setting: unset takes the built-in model,
// ModelDefault takes none ("").
func TierModel(stored, builtIn string) string {
	switch stored {
	case "":
		return builtIn
	case ModelDefault:
		return ""
	default:
		return stored
	}
}

// ResolveSessionModel is the model a new session starts with when its start
// names none: the architect model in architect mode, the implementation model
// when another session started it, otherwise none (the CLI's choice).
// architect and implementation are the tiers' models as TierModel reads them.
func ResolveSessionModel(mode SessionMode, startedBySession bool, architect, implementation string) string {
	switch {
	case mode == SessionModeArchitect:
		return architect
	case startedBySession:
		return implementation
	default:
		return ""
	}
}

// The writing helper (ADR 0026) is an assistant, not a session: one global
// model setting, haiku while unset. "default" is not offered, because the CLI's
// own default may be a slow model for a field that a person is waiting on.
const (
	// SettingModelWritingHelper is the settings key holding the writing
	// helper's model.
	SettingModelWritingHelper = "models.assistants.writing_helper"
	// DefaultWritingHelperModel is the model while the setting is unset.
	DefaultWritingHelperModel = "haiku"
)

// WritingHelperModelChoices are the values the writing helper's setting
// accepts, in the order a person is offered them.
var WritingHelperModelChoices = []string{"haiku", "sonnet", "opus", "fable"}

func validateWritingHelperModel(key, value string) error {
	if value == "" || slices.Contains(WritingHelperModelChoices, value) {
		return nil
	}
	return &StructuredError{Code: "INVALID_INPUT", Message: key + ` must be one of "haiku", "sonnet", "opus" or "fable"`}
}

// WritingHelperModel reads the writing helper's stored setting: unset, or a
// value it does not accept, takes the built-in model.
func WritingHelperModel(stored string) string {
	if slices.Contains(WritingHelperModelChoices, stored) {
		return stored
	}
	return DefaultWritingHelperModel
}

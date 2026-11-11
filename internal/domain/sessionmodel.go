package domain

import "slices"

// A session starts on the model of its session mode (ADR 0084): one
// server-wide setting per mode, so the mode alone decides, whoever started the
// session. A person's model override for one session wins over it.

// SettingModelModePrefix starts the settings key of each mode model; the mode
// value ends it, "default" for the default mode.
const SettingModelModePrefix = "models.claude.mode."

// The keys the mode models replaced. They are retired: a write is refused and
// a boot migration moves a stored value to the mode's key.
const (
	RetiredSettingModelArchitect      = "models.claude.architect"
	RetiredSettingModelImplementation = "models.claude.implementation"
)

// ModelDefault is the model setting value that leaves the choice to the CLI:
// the session starts with no --model. An unset setting is not the same: it
// takes the mode's built-in model.
const ModelDefault = "default"

// ModelChoices are the values a model setting accepts, in the order a person
// is offered them. The names are the CLI's aliases, which follow the newest
// model of each family.
var ModelChoices = []string{ModelDefault, "fable", "opus", "sonnet", "haiku"}

// modeModelModes are the session modes that have a model, in the order a
// person reads them.
var modeModelModes = []SessionMode{
	SessionModeDefault, SessionModeArchitect, SessionModeImplement,
	SessionModeDesign, SessionModeProduct, SessionModeDesignSystem,
}

// ModeModelKey is the settings key holding a mode's model.
func ModeModelKey(mode SessionMode) string {
	if mode == SessionModeDefault {
		return SettingModelModePrefix + "default"
	}
	return SettingModelModePrefix + string(mode)
}

// ModeBuiltInModel is the model a mode takes while its setting is unset: opus
// for architect, sonnet for implement, and ModelDefault (the CLI chooses) for
// the rest.
func ModeBuiltInModel(mode SessionMode) string {
	switch mode {
	case SessionModeArchitect:
		return "opus"
	case SessionModeImplement:
		return "sonnet"
	default:
		return ModelDefault
	}
}

// ModeModel is one row of the mode models read.
type ModeModel struct {
	Mode SessionMode
	Key  string
	// Current is the stored value, or the built-in while it is unset: always
	// one of ModelChoices.
	Current string
	BuiltIn string
}

// ModeModels reads every mode's model through read, which returns a setting's
// stored value ("" while unset), in the order a person reads the modes.
func ModeModels(read func(key string) string) []ModeModel {
	out := make([]ModeModel, 0, len(modeModelModes))
	for _, mode := range modeModelModes {
		key, builtIn := ModeModelKey(mode), ModeBuiltInModel(mode)
		current := read(key)
		if current == "" {
			current = builtIn
		}
		out = append(out, ModeModel{Mode: mode, Key: key, Current: current, BuiltIn: builtIn})
	}
	return out
}

// retiredModelKeys maps each retired key to the key that replaced it.
var retiredModelKeys = map[string]string{
	RetiredSettingModelArchitect:      ModeModelKey(SessionModeArchitect),
	RetiredSettingModelImplementation: ModeModelKey(SessionModeImplement),
}

// RetiredModelKeys maps each retired model key to the mode key that replaced it.
func RetiredModelKeys() map[string]string {
	out := make(map[string]string, len(retiredModelKeys))
	for old, key := range retiredModelKeys {
		out[old] = key
	}
	return out
}

func validateModelSetting(key, value string) error {
	if value == "" || slices.Contains(ModelChoices, value) {
		return nil
	}
	return &StructuredError{Code: "INVALID_INPUT", Message: key + ` must be one of "default", "fable", "opus", "sonnet" or "haiku"`}
}

// ValidModelChoice is nil when value is a model a start may name: one of
// ModelChoices.
func ValidModelChoice(field, value string) error {
	if !slices.Contains(ModelChoices, value) {
		return &StructuredError{Code: "INVALID_INPUT", Message: field + ` must be one of "default", "fable", "opus", "sonnet" or "haiku"`}
	}
	return nil
}

// ResolveSessionModel is the --model a new session starts with: the override
// when the start names one, else the mode's stored setting, else the mode's
// built-in. ModelDefault at either step is no model (""): the CLI chooses.
func ResolveSessionModel(mode SessionMode, override, stored string) string {
	model := override
	if model == "" {
		model = stored
	}
	if model == "" {
		model = ModeBuiltInModel(mode)
	}
	if model == ModelDefault {
		return ""
	}
	return model
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

// The architecture assistant is the second assistant: one global model
// setting, sonnet while unset, with the same choices as the writing helper.
const (
	// SettingModelArchitectureAssistant is the settings key holding the
	// architecture assistant's model.
	SettingModelArchitectureAssistant = "models.assistants.architecture"
	// DefaultArchitectureAssistantModel is the model while the setting is unset.
	DefaultArchitectureAssistantModel = "sonnet"
)

// ArchitectureAssistantModel reads the architecture assistant's stored
// setting: unset, or a value it does not accept, takes the built-in model.
func ArchitectureAssistantModel(stored string) string {
	if slices.Contains(WritingHelperModelChoices, stored) {
		return stored
	}
	return DefaultArchitectureAssistantModel
}

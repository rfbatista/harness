package domain

import (
	"errors"
	"strings"
	"testing"
)

// The model a session starts with is the override, else its mode's stored
// setting, else the mode's built-in; "default" at either step is no model.
// Who started the session is not an input.
func TestResolveSessionModel(t *testing.T) {
	cases := []struct {
		name     string
		mode     SessionMode
		override string
		stored   string
		want     string
	}{
		{"architect unset", SessionModeArchitect, "", "", "opus"},
		{"implement unset", SessionModeImplement, "", "", "sonnet"},
		{"default unset", SessionModeDefault, "", "", ""},
		{"design unset", SessionModeDesign, "", "", ""},
		{"product unset", SessionModeProduct, "", "", ""},
		{"design system unset", SessionModeDesignSystem, "", "", ""},
		{"stored wins over built-in", SessionModeArchitect, "", "fable", "fable"},
		{"stored default means the CLI chooses", SessionModeImplement, "", ModelDefault, ""},
		{"a row set on a mode without a built-in", SessionModeDesign, "", "haiku", "haiku"},
		{"override wins over stored", SessionModeArchitect, "haiku", "fable", "haiku"},
		{"override default beats a pinned mode", SessionModeArchitect, ModelDefault, "fable", ""},
	}
	for _, c := range cases {
		if got := ResolveSessionModel(c.mode, c.override, c.stored); got != c.want {
			t.Errorf("%s: ResolveSessionModel(%q, %q, %q) = %q; want %q", c.name, c.mode, c.override, c.stored, got, c.want)
		}
	}
}

// One row per mode, in the contract's order: the stored value or the built-in
// while unset, always one of the choices.
func TestModeModels(t *testing.T) {
	stored := map[string]string{"models.claude.mode.design": "haiku", "models.claude.mode.implement": ModelDefault}
	got := ModeModels(func(k string) string { return stored[k] })
	want := []ModeModel{
		{SessionModeDefault, "models.claude.mode.default", "default", "default"},
		{SessionModeArchitect, "models.claude.mode.architect", "opus", "opus"},
		{SessionModeImplement, "models.claude.mode.implement", "default", "sonnet"},
		{SessionModeDesign, "models.claude.mode.design", "haiku", "default"},
		{SessionModeProduct, "models.claude.mode.product", "default", "default"},
		{SessionModeDesignSystem, "models.claude.mode.designsystem", "default", "default"},
	}
	if len(got) != len(want) {
		t.Fatalf("ModeModels = %+v; want %d rows", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestValidateModeModelSettings(t *testing.T) {
	for _, m := range ModeModels(func(string) string { return "" }) {
		for _, v := range append([]string{""}, ModelChoices...) {
			if err := ValidateSetting(m.Key, v); err != nil {
				t.Errorf("ValidateSetting(%q, %q) = %v; want accepted", m.Key, v, err)
			}
		}
		err := ValidateSetting(m.Key, "gpt-4")
		var se *StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" || !strings.Contains(se.Message, m.Key) || !strings.Contains(se.Message, `"haiku"`) {
			t.Errorf("ValidateSetting(%q, gpt-4) = %v; want INVALID_INPUT naming the key and the choices", m.Key, err)
		}
	}
}

// A retired key is refused, and the message names its new key.
func TestRetiredModelKeysAreRefused(t *testing.T) {
	for old, replacement := range map[string]string{
		"models.claude.architect":      "models.claude.mode.architect",
		"models.claude.implementation": "models.claude.mode.implement",
	} {
		err := ValidateSetting(old, "haiku")
		var se *StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" || !strings.Contains(se.Message, replacement) {
			t.Errorf("ValidateSetting(%q) = %v; want INVALID_INPUT naming %q", old, err, replacement)
		}
	}
}

func TestValidateWritingHelperModelSetting(t *testing.T) {
	for _, v := range []string{"", "haiku", "sonnet", "opus", "fable"} {
		if err := ValidateSetting(SettingModelWritingHelper, v); err != nil {
			t.Errorf("ValidateSetting(writing helper, %q) = %v; want accepted", v, err)
		}
	}
	for _, v := range []string{ModelDefault, "gpt-4", "Haiku"} {
		err := ValidateSetting(SettingModelWritingHelper, v)
		var se *StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
			t.Errorf("ValidateSetting(writing helper, %q) = %v; want INVALID_INPUT", v, err)
		}
	}
}

func TestValidateArchitectureAssistantModelSetting(t *testing.T) {
	for _, v := range []string{"", "haiku", "sonnet", "opus", "fable"} {
		if err := ValidateSetting(SettingModelArchitectureAssistant, v); err != nil {
			t.Errorf("ValidateSetting(architecture assistant, %q) = %v; want accepted", v, err)
		}
	}
	for _, v := range []string{ModelDefault, "gpt-4", "Sonnet"} {
		err := ValidateSetting(SettingModelArchitectureAssistant, v)
		var se *StructuredError
		if !errors.As(err, &se) || se.Code != "INVALID_INPUT" {
			t.Errorf("ValidateSetting(architecture assistant, %q) = %v; want INVALID_INPUT", v, err)
		}
	}
}

func TestTheArchitectureAssistantModelIsSonnetWhileUnset(t *testing.T) {
	for stored, want := range map[string]string{"": "sonnet", "opus": "opus", "default": "sonnet", "gpt-4": "sonnet"} {
		if got := ArchitectureAssistantModel(stored); got != want {
			t.Errorf("ArchitectureAssistantModel(%q) = %q, want %q", stored, got, want)
		}
	}
}

package domain

import "testing"

// Architect mode wins even when a session started the architect; otherwise a
// session started by another session gets the implementation model, and one a
// person started gets none.
func TestResolveSessionModel(t *testing.T) {
	cases := []struct {
		mode             SessionMode
		startedBySession bool
		want             string
	}{
		{SessionModeArchitect, false, "opus"},
		{SessionModeArchitect, true, "opus"},
		{SessionModeImplement, true, "sonnet"},
		{SessionModeDefault, true, "sonnet"},
		{SessionModeDesign, true, "sonnet"},
		{SessionModeDefault, false, ""},
		{SessionModeImplement, false, ""},
		{SessionModeProduct, false, ""},
	}
	for _, c := range cases {
		if got := ResolveSessionModel(c.mode, c.startedBySession, "opus", "sonnet"); got != c.want {
			t.Errorf("ResolveSessionModel(%q, %v) = %q; want %q", c.mode, c.startedBySession, got, c.want)
		}
	}
}

// Unset takes the tier's built-in model; "default" takes none, so the CLI
// chooses.
func TestTierModel(t *testing.T) {
	for stored, want := range map[string]string{"": "opus", ModelDefault: "", "haiku": "haiku"} {
		if got := TierModel(stored, "opus"); got != want {
			t.Errorf("TierModel(%q) = %q; want %q", stored, got, want)
		}
	}
}

func TestValidateModelSettings(t *testing.T) {
	for _, key := range []string{SettingModelArchitect, SettingModelImplementation} {
		for _, v := range append([]string{""}, ModelChoices...) {
			if err := ValidateSetting(key, v); err != nil {
				t.Errorf("ValidateSetting(%q, %q) = %v; want accepted", key, v, err)
			}
		}
		if err := ValidateSetting(key, "gpt-4"); err == nil {
			t.Errorf("ValidateSetting(%q, gpt-4) accepted; want refused", key)
		}
	}
}

package domain

import "testing"

func TestCleanEnvFilePath(t *testing.T) {
	ok := map[string]string{
		".env":                ".env",
		" apps/api/.env ":     "apps/api/.env",
		"./config/.env.local": "config/.env.local",
		"a/../b/.env":         "b/.env",
		`apps\web\.env`:       "apps/web/.env",
	}
	for in, want := range ok {
		if got, err := CleanEnvFilePath(in); err != nil || got != want {
			t.Errorf("CleanEnvFilePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "~/.env", "../.env", "a/../../.env", ".git/config", ".", ".git"} {
		if _, err := CleanEnvFilePath(bad); err == nil || err.(*StructuredError).Code != "INVALID_PATH" {
			t.Errorf("CleanEnvFilePath(%q): want INVALID_PATH, got %v", bad, err)
		}
	}
}

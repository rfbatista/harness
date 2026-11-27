package app

import (
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/adapter/out/personcred"
)

func TestTheHarnessSecretsAreTheDatabaseFilesTheKeyAndThePersonCredentialFile(t *testing.T) {
	dir := t.TempDir()
	got := harnessSecrets(Config{DBPath: filepath.Join(dir, "data.db"), DataDir: dir})
	cred, err := personcred.Dir()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{filepath.Join(dir, "data.db"), filepath.Join(dir, "data.db-wal"), filepath.Join(dir, "data.db-shm"), filepath.Join(dir, "secret.key"), cred} {
		if !slices.Contains(got, want) {
			t.Errorf("%s is not among the secrets %v", want, got)
		}
	}
	if len(harnessSecrets(Config{DBPath: ":memory:"})) == 0 {
		t.Error("an in-memory database dropped the other secrets")
	}
}

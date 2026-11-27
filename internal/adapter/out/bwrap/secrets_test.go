package bwrap

import (
	"os"
	"path/filepath"
	"testing"

	"operators-mcp/internal/ports"
)

// Secrets (ADR 0016) are masked over every bind, so a database inside a
// repository the session may read cannot be read through it.

func TestASecretFileInsideAGrantedRepositoryIsMaskedAfterTheBindThatShowsIt(t *testing.T) {
	repo := t.TempDir()
	db := filepath.Join(repo, "data.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(repo, "keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	args := Render(ports.SandboxPolicy{
		WorkingDir: repo,
		Readable:   []string{repo},
		Secrets:    []string{db, db + "-wal", keyDir},
	}, "/home/me")

	bind := index(args, "--ro-bind-try", repo, repo)
	mask := index(args, "--ro-bind", "/dev/null", db)
	if bind < 0 || mask < 0 {
		t.Fatalf("bind %d mask %d in %v", bind, mask, args)
	}
	if mask < bind {
		t.Errorf("the mask comes before the bind that shows the file, so the bind covers it: %v", args)
	}
	// A folder is masked with an empty tmpfs.
	if tm, b := index(args, "--tmpfs", keyDir), bind; tm < b {
		t.Errorf("the folder mask (%d) is not after the bind (%d): %v", tm, b, args)
	}
	// A secret that does not exist is not bound: bubblewrap cannot mount over
	// a path it cannot create in a read-only parent.
	if has(args, "--ro-bind", "/dev/null", db+"-wal") {
		t.Errorf("a file that does not exist was masked: %v", args)
	}
}

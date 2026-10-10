package secretbox_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/domain"
)

func TestSealsAndOpensWithAKeyFileItCreatesOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "secret.key")
	b, err := secretbox.New("", path)
	if err != nil {
		t.Fatal(err)
	}
	if b.KeyID() != "" {
		t.Fatalf("KeyID = %q before any key, want empty", b.KeyID())
	}
	if _, _, err := b.Seal([]byte("x"), false); errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("Seal without create and without a key: %v, want STORE_LOCKED", err)
	}
	sealed, id, err := b.Seal([]byte("ghp_secret"), true)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || id != b.KeyID() {
		t.Fatalf("Seal key id %q, box key id %q", id, b.KeyID())
	}
	if bytes.Contains(sealed, []byte("ghp_secret")) {
		t.Fatal("the sealed bytes carry the secret in clear")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %o, want 600", info.Mode().Perm())
	}

	// A box over the same file opens it: the key outlives the process.
	again, err := secretbox.New("", path)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := again.Open(sealed)
	if err != nil || string(plain) != "ghp_secret" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if again.KeyID() != id {
		t.Fatalf("reopened key id %q, want %q", again.KeyID(), id)
	}
}

func TestAWrongOrMissingKeyIsTheLockedStore(t *testing.T) {
	dir := t.TempDir()
	b, err := secretbox.New("", filepath.Join(dir, "a.key"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, _, err := b.Seal([]byte("token"), true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := secretbox.New("", filepath.Join(dir, "b.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("Open without a key: %v, want STORE_LOCKED", err)
	}
	if _, _, err := other.Seal([]byte("y"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("Open with another key: %v, want STORE_LOCKED", err)
	}
}

func TestTheEnvironmentKeyWinsAndIsValidated(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	path := filepath.Join(t.TempDir(), "secret.key")
	b, err := secretbox.New(key, path)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _, err := b.Seal([]byte("s"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an env key wrote a key file: %v", err)
	}
	same, _ := secretbox.New(key, path)
	if plain, err := same.Open(sealed); err != nil || string(plain) != "s" {
		t.Fatalf("Open = %q, %v", plain, err)
	}
	if _, err := secretbox.New("not-base64!", path); err == nil {
		t.Fatal("a malformed env key was accepted")
	}
	if _, err := secretbox.New(base64.StdEncoding.EncodeToString([]byte("short")), path); err == nil {
		t.Fatal("a short env key was accepted")
	}
}

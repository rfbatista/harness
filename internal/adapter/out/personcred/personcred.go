// Package personcred keeps the person credential (ADR 0016) on the person's
// machine for the clients that send it: the terminal client and the bench
// command. It is never passed in an argument or an environment variable, which
// any process of the user can read (KERN_PROCARGS2 on macOS), and it is read at
// the moment a client starts, so a session that only runs inside the client never
// holds it.
//
// It lives in one 0600 file, ~/.coding-pool/person.cred, in a 0700 folder: a path
// the harness puts in every sandbox profile's secrets. There is no keychain
// path: `security` is reachable by a session by its full path and could hand the
// item back without a prompt.
package personcred

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrNone says no credential is stored: the client has not been paired.
var ErrNone = errors.New("no person credential is stored: pair this client with the harness first")

// Store keeps one credential.
type Store interface {
	Load() (string, error)
	Save(credential string) error
}

// Default is the store the clients use. Tests replace it.
var Default Store = FileStore{}

// Load answers the stored credential, ErrNone when there is none.
func Load() (string, error) { return Default.Load() }

// Save stores the credential, replacing the one before it.
func Save(credential string) error { return Default.Save(credential) }

// Dir is ~/.coding-pool, the folder the harness's own client state lives in. It
// is the secret every sandbox profile denies, the credential's temporary file
// beside it included.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coding-pool"), nil
}

// Path is the credential file, ~/.coding-pool/person.cred.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "person.cred"), nil
}

// FileStore keeps the credential in a 0600 file.
type FileStore struct{ PathFn func() (string, error) }

func (f FileStore) path() (string, error) {
	if f.PathFn != nil {
		return f.PathFn()
	}
	return Path()
}

// Load implements Store.
func (f FileStore) Load() (string, error) {
	p, err := f.path()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNone
	}
	if err != nil {
		return "", err
	}
	if c := strings.TrimSpace(string(b)); c != "" {
		return c, nil
	}
	return "", ErrNone
}

// Save implements Store.
func (f FileStore) Save(credential string) error {
	p, err := f.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(credential)+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

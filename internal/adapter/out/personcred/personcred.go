// Package personcred keeps the person credential (ADR 0016) on the person's
// machine for the clients that send it: the terminal client and the bench
// command. It is never passed in an argument or an environment variable, which
// any process of the user can read (KERN_PROCARGS2 on macOS), and it is read at
// the moment a client starts, so a session that only runs inside the client never
// holds it.
//
// It lives in one 0600 file, ~/.coding-pool/person/person.cred, in a 0700 folder of
// its own: a folder the harness puts in every sandbox profile's secrets. There is no keychain
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

// Dir is ~/.coding-pool/person, a folder of its own: it holds the credential
// file and its temporary file and nothing else, and it is the secret every
// sandbox profile denies. ~/.coding-pool itself also holds every session's
// worktree, so it cannot be one.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coding-pool", "person"), nil
}

// legacyPath is where an earlier build kept the credential.
func legacyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coding-pool", "person.cred"), nil
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

// migrate moves the credential an earlier build kept in ~/.coding-pool/person.cred
// into the folder of its own, once: a credential already there is never replaced
// by an old file. It does nothing for a store with a path of its own (tests).
func (f FileStore) migrate(p string) {
	if f.PathFn != nil {
		return
	}
	if _, err := os.Stat(p); err == nil {
		return
	}
	old, err := legacyPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(old)
	if err != nil {
		return
	}
	if f.Save(string(b)) == nil {
		_ = os.Remove(old)
	}
}

// Load implements Store.
func (f FileStore) Load() (string, error) {
	p, err := f.path()
	if err != nil {
		return "", err
	}
	f.migrate(p)
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

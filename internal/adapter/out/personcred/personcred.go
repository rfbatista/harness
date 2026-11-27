// Package personcred keeps the person credential (ADR 0016) on the person's
// machine for the clients that send it: the terminal client and the bench
// command. It is never passed in an argument or an environment variable, which
// any process of the user can read (KERN_PROCARGS2 on macOS), and it is read at
// the moment a client starts, so a session that only runs inside the client never
// holds it.
//
// On macOS the credential lives in the login keychain, written and read through
// /usr/bin/security with the secret on stdin and stdout, never in an argument;
// anywhere else, or when the keychain cannot be used, it lives in a 0600 file in
// the harness's own directory (Path), which every sandbox profile hides.
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
var Default Store = platformStore()

// Load answers the stored credential, ErrNone when there is none.
func Load() (string, error) { return Default.Load() }

// Save stores the credential, replacing the one before it.
func Save(credential string) error { return Default.Save(credential) }

// Path is the credential file: ~/.coding-pool/person.cred, in the directory the
// harness's own client state lives in. It is a path the sandbox hides.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coding-pool", "person.cred"), nil
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

// Package secretbox seals the harness's stored secrets with AES-256-GCM. The
// key never sits in the database: it comes from HARNESS_SECRET_KEY (base64,
// 32 bytes) or else from a key file (mode 0600) the box creates the first
// time it is asked to, which is only when nothing is sealed yet.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SecretBox = (*Box)(nil)

// EnvKey is the environment variable that carries the key.
const EnvKey = "HARNESS_SECRET_KEY"

const keySize = 32

// Box is ports.SecretBox over one key.
type Box struct {
	path string

	mu  sync.Mutex
	key []byte // nil: no key yet
	id  string
}

// New returns a box keyed by envValue when it is set, else by the file at
// path (read now if it exists, created on the first Seal that may create).
// A malformed envValue is an error: the server should not start on a key it
// cannot use.
func New(envValue, path string) (*Box, error) {
	b := &Box{path: path}
	if v := strings.TrimSpace(envValue); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(key) != keySize {
			return nil, fmt.Errorf("%s must be %d bytes, base64-encoded", EnvKey, keySize)
		}
		b.set(key)
		return b, nil
	}
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return b, nil
	case err != nil:
		return nil, fmt.Errorf("read secret key: %w", err)
	case len(key) != keySize:
		return nil, fmt.Errorf("secret key %s is %d bytes, want %d", path, len(key), keySize)
	}
	b.set(key)
	return b, nil
}

func (b *Box) set(key []byte) {
	sum := sha256.Sum256(key)
	b.key, b.id = key, hex.EncodeToString(sum[:8])
}

// KeyID names the key, "" while there is none.
func (b *Box) KeyID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id
}

// Seal encrypts plain; nonce first, then the ciphertext.
func (b *Box) Seal(plain []byte, create bool) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.key == nil {
		if !create {
			return nil, "", domain.ErrGitCredentialStoreLocked
		}
		if err := b.createKey(); err != nil {
			return nil, "", err
		}
	}
	aead, err := b.aead()
	if err != nil {
		return nil, "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}
	return aead.Seal(nonce, nonce, plain, nil), b.id, nil
}

// Open decrypts what Seal sealed; a key that does not open it is the
// locked store.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.key == nil {
		return nil, domain.ErrGitCredentialStoreLocked
	}
	aead, err := b.aead()
	if err != nil {
		return nil, err
	}
	n := aead.NonceSize()
	if len(sealed) < n {
		return nil, domain.ErrGitCredentialStoreLocked
	}
	plain, err := aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, domain.ErrGitCredentialStoreLocked
	}
	return plain, nil
}

func (b *Box) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// createKey writes a new random key to the key file, never over one that
// exists.
func (b *Box) createKey() error {
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(b.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create secret key: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	b.set(key)
	return nil
}

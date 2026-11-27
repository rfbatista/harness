// Package personproof is the person credential (ADR 0016): the pairing code the
// harness prints once, the credentials it issues for it, and the check of a
// credential a person-only route asks for.
package personproof

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.PersonVerifier = (*Service)(nil)
	_ ports.PersonPairing  = (*Service)(nil)
)

// CodeTTL is how long a pairing code stays valid.
const CodeTTL = 24 * time.Hour

// Service issues and checks person credentials.
type Service struct {
	store ports.PersonCredentials
	now   func() time.Time
	// Events, when set, is told of every credential issued.
	Events ports.EventPublisher

	mu      sync.Mutex
	code    string
	expires time.Time
}

// New returns the service over store.
func New(store ports.PersonCredentials) *Service { return &Service{store: store, now: time.Now} }

// SetClock replaces the clock, for tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

func randomString(n int, enc func([]byte) string) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("personproof: no randomness: " + err.Error())
	}
	return enc(b)
}

func hash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}

// NewCode implements ports.PersonPairing.
func (s *Service) NewCode() string {
	code := randomString(16, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString)
	s.mu.Lock()
	s.code, s.expires = code, s.now().Add(CodeTTL)
	s.mu.Unlock()
	return code
}

// FirstCode makes a pairing code when no credential has been issued yet, and
// answers it; ok is false when someone is paired already.
func (s *Service) FirstCode(ctx context.Context) (code string, ok bool) {
	n, err := s.store.Count(ctx)
	if err != nil || n > 0 {
		return "", false
	}
	return s.NewCode(), true
}

// Redeem implements ports.PersonPairing.
func (s *Service) Redeem(ctx context.Context, code string) (string, error) {
	s.mu.Lock()
	want, expires := s.code, s.expires
	valid := want != "" && s.now().Before(expires) && subtle.ConstantTimeCompare([]byte(code), []byte(want)) == 1
	if valid {
		s.code = "" // one use
	}
	s.mu.Unlock()
	if !valid {
		return "", &domain.StructuredError{Code: "PAIRING_CODE_INVALID", Message: "that pairing code is wrong, used or expired; the harness prints a new one when it starts with nobody paired"}
	}
	credential := randomString(32, hex.EncodeToString)
	if err := s.store.Add(ctx, hash(credential)); err != nil {
		return "", err
	}
	if s.Events != nil {
		_ = s.Events.Publish(ctx, domain.PersonCredentialIssued{Client: ports.PersonClientFrom(ctx), At: s.now()})
	}
	return credential, nil
}

// Verify implements ports.PersonVerifier.
func (s *Service) Verify(ctx context.Context, credential string) bool {
	if credential == "" {
		return false
	}
	ok, err := s.store.Has(ctx, hash(credential))
	return err == nil && ok
}

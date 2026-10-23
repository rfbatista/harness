// Package agentcredential keeps the credential agent claude processes sign in
// with (ADR 0018): an OAuth token or an Anthropic API key, sealed with the
// harness's secret key. The value leaves the package only as an env entry for
// an agent process (Env); status never carries it. Nothing here reads the
// person's own login or keychain.
package agentcredential

import (
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The env variables claude reads for each kind.
const (
	EnvOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
	EnvAPIKey     = "ANTHROPIC_API_KEY"
)

var _ ports.AgentCredential = (*Service)(nil)

// Service is the agent credential over a store and a secret box.
type Service struct {
	store ports.AgentCredentialStore
	box   ports.SecretBox
	now   func() time.Time
	mu    sync.Mutex
}

// New builds the service; a nil now is time.Now.
func New(store ports.AgentCredentialStore, box ports.SecretBox, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, box: box, now: now}
}

func (s *Service) Status() ports.AgentCredentialStatus {
	rec, err := s.store.Get()
	if err != nil || rec == nil {
		return ports.AgentCredentialStatus{}
	}
	return ports.AgentCredentialStatus{Set: true, Kind: rec.Kind, Updated: rec.Updated}
}

func (s *Service) Set(kind, value string) (ports.AgentCredentialStatus, error) {
	if kind != ports.AgentCredentialOAuth && kind != ports.AgentCredentialAPIKey {
		return ports.AgentCredentialStatus{}, invalid("Choose OAuth token or Anthropic API key.")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ports.AgentCredentialStatus{}, invalid("Paste a token or key.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, keyID, err := s.box.Seal([]byte(value), true)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	if err := s.store.Put(ports.AgentCredentialRecord{Kind: kind, Sealed: sealed, KeyID: keyID, Updated: s.now().UTC()}); err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	return s.Status(), nil
}

func (s *Service) Remove() (ports.AgentCredentialStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Delete(); err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	return ports.AgentCredentialStatus{}, nil
}

// Env is the credential as env entries for an agent process: the variable
// that matches its kind. Nil when none is set or it cannot be opened.
func (s *Service) Env() []string {
	rec, err := s.store.Get()
	if err != nil || rec == nil || rec.KeyID != s.box.KeyID() {
		return nil
	}
	plain, err := s.box.Open(rec.Sealed)
	if err != nil {
		return nil
	}
	switch rec.Kind {
	case ports.AgentCredentialOAuth:
		return []string{EnvOAuthToken + "=" + string(plain)}
	case ports.AgentCredentialAPIKey:
		return []string{EnvAPIKey + "=" + string(plain)}
	}
	return nil
}

func invalid(message string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: message}
}

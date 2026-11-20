// Package agentcredential keeps the credential agent claude processes sign in
// with (ADR 0018): an OAuth token or an Anthropic API key, sealed with the
// harness's secret key. The value leaves the package only as an env entry for
// an agent process (Env); status never carries it. Nothing here reads the
// person's own login or keychain.
package agentcredential

import (
	"context"
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

var (
	_ ports.AgentCredential         = (*Service)(nil)
	_ ports.ProjectAgentCredentials = (*Service)(nil)
)

// Service is the agent credential over a store and a secret box.
type Service struct {
	store ports.AgentCredentialStore
	// projects holds the per-project credentials; nil keeps only the global one.
	projects ports.ProjectAgentCredentialStore
	box      ports.SecretBox
	now      func() time.Time
	mu       sync.Mutex
	// logins are the project Claude homes' logins, globalAuth the agent
	// home's, and globalLogin says whether it holds one; all nil without a
	// sign-in (ADR 0102).
	logins      ports.ProjectAgentOAuth
	globalAuth  ports.AgentAuth
	globalLogin func() bool
}

// New builds the service; a nil now is time.Now.
func New(store ports.AgentCredentialStore, box ports.SecretBox, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, box: box, now: now}
}

// WithProjects turns on per-project credentials and answers the service.
func (s *Service) WithProjects(store ports.ProjectAgentCredentialStore) *Service {
	s.projects = store
	return s
}

// WithLogins adds the logins the credential can be: each project's own, kept in
// its project Claude home, and the agent login in the agent home (globalLogin
// says whether it holds one; globalAuth tells who it is signed in as). It
// answers the service.
func (s *Service) WithLogins(projects ports.ProjectAgentOAuth, globalAuth ports.AgentAuth, globalLogin func() bool) *Service {
	s.logins, s.globalAuth, s.globalLogin = projects, globalAuth, globalLogin
	return s
}

func (s *Service) Status() ports.AgentCredentialStatus {
	rec, err := s.store.Get()
	if err != nil || rec == nil {
		return ports.AgentCredentialStatus{}
	}
	return ports.AgentCredentialStatus{Set: true, Kind: rec.Kind, Updated: rec.Updated}
}

func (s *Service) Set(kind, value string) (ports.AgentCredentialStatus, error) {
	rec, err := s.seal(kind, value)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.Put(rec); err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	return s.Status(), nil
}

// seal validates a pasted credential and seals it into a record.
func (s *Service) seal(kind, value string) (ports.AgentCredentialRecord, error) {
	if kind != ports.AgentCredentialOAuth && kind != ports.AgentCredentialAPIKey {
		return ports.AgentCredentialRecord{}, invalid("Choose OAuth token or Anthropic API key.")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ports.AgentCredentialRecord{}, invalid("Paste a token or key.")
	}
	sealed, keyID, err := s.box.Seal([]byte(value), true)
	if err != nil {
		return ports.AgentCredentialRecord{}, err
	}
	return ports.AgentCredentialRecord{Kind: kind, Sealed: sealed, KeyID: keyID, Updated: s.now().UTC()}, nil
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
	if err != nil {
		return nil
	}
	return s.env(rec)
}

// env is a record as env entries; nil for no record or one this box cannot open.
func (s *Service) env(rec *ports.AgentCredentialRecord) []string {
	if rec == nil || rec.KeyID != s.box.KeyID() {
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

var errProjectsOff = &domain.StructuredError{Code: "UNAVAILABLE", Message: "per-project agent credentials are not configured on this server"}

func (s *Service) projectStore(projectID string) (ports.ProjectAgentCredentialStore, error) {
	if s.projects == nil {
		return nil, errProjectsOff
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, invalid("A project is required.")
	}
	return s.projects, nil
}

// StatusFor is whether the project has a credential of its own; the global one
// is not counted.
func (s *Service) StatusFor(projectID string) (ports.AgentCredentialStatus, error) {
	st, err := s.projectStore(projectID)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	rec, err := st.GetFor(projectID)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	if rec == nil {
		return ports.AgentCredentialStatus{}, nil
	}
	return ports.AgentCredentialStatus{Set: true, Kind: rec.Kind, Updated: rec.Updated}, nil
}

func (s *Service) SetFor(projectID, kind, value string) (ports.AgentCredentialStatus, error) {
	st, err := s.projectStore(projectID)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	rec, err := s.seal(kind, value)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	// A project never holds a login and a pasted credential: the login is
	// signed out of its home first, and a failed sign-out saves nothing.
	signedOut, err := s.signOutLogin(projectID)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := st.PutFor(projectID, rec); err != nil {
		if s.logins != nil && signedOut {
			return ports.AgentCredentialStatus{}, &domain.StructuredError{Code: "AGENT_CREDENTIAL_NOT_SAVED",
				Message: "The project was signed out of Claude, but the pasted credential could not be saved: " + err.Error()}
		}
		return ports.AgentCredentialStatus{}, err
	}
	return ports.AgentCredentialStatus{Set: true, Kind: rec.Kind, Updated: rec.Updated}, nil
}

// loginTimeout bounds a `claude auth status` or `claude auth logout`.
const loginTimeout = 30 * time.Second

// signOutLogin runs `claude auth logout` in the project's Claude home when it
// holds a login, and answers whether it did. A logout that exits cleanly and
// leaves the login is a failed sign-out: the exit code is not trusted alone.
func (s *Service) signOutLogin(projectID string) (bool, error) {
	if s.logins == nil || !s.logins.HasLogin(projectID) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()
	if err := s.logins.For(projectID).Logout(ctx); err != nil {
		return false, err
	}
	if s.logins.HasLogin(projectID) {
		return false, &domain.StructuredError{Code: "AGENT_SIGN_OUT_FAILED", Message: "claude signed out but the project's login is still there."}
	}
	return true, nil
}

// DetailFor is the project's credential: a login when its Claude home holds
// one, else its pasted credential, with the credential its next claude
// process runs with.
func (s *Service) DetailFor(ctx context.Context, projectID string) (ports.ProjectCredentialStatus, error) {
	own, err := s.StatusFor(projectID)
	if err != nil {
		return ports.ProjectCredentialStatus{}, err
	}
	d := ports.ProjectCredentialStatus{AgentCredentialStatus: own}
	switch {
	case s.logins != nil && s.logins.HasLogin(projectID):
		d.AgentCredentialStatus = ports.AgentCredentialStatus{Set: true, Kind: ports.AgentCredentialLogin}
		d.Account, d.Subscription, d.CheckedOK = s.loginWho(ctx, s.logins.For(projectID))
		d.Effective = ports.EffectiveCredential{Source: ports.EffectiveProject, Kind: ports.AgentCredentialLogin, Account: d.Account, Subscription: d.Subscription}
	case own.Set:
		d.Effective = ports.EffectiveCredential{Source: ports.EffectiveProject, Kind: own.Kind}
	case s.globalLogin != nil && s.globalLogin():
		who := ports.EffectiveCredential{Source: ports.EffectiveGlobalLogin, Kind: ports.AgentCredentialLogin}
		if s.globalAuth != nil {
			who.Account, who.Subscription, _ = s.loginWho(ctx, s.globalAuth)
		}
		d.Effective = who
	default:
		if g := s.Status(); g.Set {
			d.Effective = ports.EffectiveCredential{Source: ports.EffectiveGlobalPasted, Kind: g.Kind}
		} else {
			d.Effective = ports.EffectiveCredential{Source: ports.EffectiveNone}
		}
	}
	return d, nil
}

// loginWho asks `claude auth status` who a login is; ok false when it could
// not be read.
func (s *Service) loginWho(ctx context.Context, auth ports.AgentAuth) (account, subscription string, ok bool) {
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	st, err := auth.Status(ctx)
	if err != nil {
		return "", "", false
	}
	return st.Email, st.SubscriptionType, true
}

func (s *Service) RemoveFor(projectID string) (ports.AgentCredentialStatus, error) {
	st, err := s.projectStore(projectID)
	if err != nil {
		return ports.AgentCredentialStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return ports.AgentCredentialStatus{}, st.DeleteFor(projectID)
}

// EnvFor is the credential for an agent process of the project: the project's
// own when it has one, otherwise the global one (Env). A project credential
// this box cannot open yields nothing rather than silently using the global
// one, so a project never runs under an identity it did not choose.
func (s *Service) EnvFor(projectID string) []string {
	if s.projects != nil && projectID != "" {
		rec, err := s.projects.GetFor(projectID)
		if err != nil {
			return nil
		}
		if rec != nil {
			return s.env(rec)
		}
	}
	return s.Env()
}

// OwnsEnv says whether the project has a credential of its own, which then wins
// over a login in the agent home.
func (s *Service) OwnsEnv(projectID string) bool {
	if s.projects == nil || projectID == "" {
		return false
	}
	rec, err := s.projects.GetFor(projectID)
	return err == nil && rec != nil
}

// Subscribe drops a deleted project's credential.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	if s.projects == nil {
		return
	}
	ports.On(sub, func(_ context.Context, ev domain.ProjectDeleted) error {
		return s.projects.DeleteFor(ev.ProjectID)
	})
}

func invalid(message string) error {
	return &domain.StructuredError{Code: "INVALID_INPUT", Message: message}
}

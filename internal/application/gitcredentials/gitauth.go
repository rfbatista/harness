package gitcredentials

import (
	"context"
	"errors"
	"log/slog"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitAuth = (*Service)(nil)

// githubHost is where gh and the pull requests go when origin is not on a
// code host the harness knows.
const githubHost = "github.com"

// liveSession is a session with a git directory: what its sockets resolve
// from on every request, and its gitconfig is rewritten from when the
// identity changes. The origin is read once, when the session starts or
// resumes.
type liveSession struct {
	repositoryID string
	origin       string
}

// resolve is what remote operations on repositoryID (with origin) use now.
func (s *Service) resolve(repositoryID, origin string) (*ports.GitResolution, []*ports.StoredGitCredential, error) {
	all, err := s.st.Credentials.List()
	if err != nil {
		return nil, nil, err
	}
	pinned := ""
	if repositoryID != "" {
		if pinned, err = s.st.Pins.Get(repositoryID); err != nil {
			return nil, nil, err
		}
	}
	creds := publicOf(all)
	res := &ports.GitResolution{}
	if u, ok := domain.ParseGitRemoteURL(origin); ok {
		res.Remote = &u
	}
	res.Push = domain.ResolveGitCredential(creds, pinned, res.Remote)
	apiHost := githubHost
	if res.Remote != nil {
		apiHost = res.Remote.Host
	}
	if res.Push != nil && res.Push.Kind == domain.GitHTTPSToken && res.Push.Host == apiHost {
		res.GitHub = res.Push
	} else {
		res.GitHub = domain.CredentialFor(creds, apiHost, domain.GitHTTPSToken)
	}
	return res, all, nil
}

// Resolve reads the store only.
func (s *Service) Resolve(_ context.Context, repositoryID, originURL string) (*ports.GitResolution, error) {
	res, _, err := s.resolve(repositoryID, originURL)
	return res, err
}

// material is what a git directory for res holds; locked is true when the
// secrets could not be read (the material then holds none).
func (s *Service) material(res *ports.GitResolution, all []*ports.StoredGitCredential) (ports.GitAuthMaterial, bool, error) {
	var m ports.GitAuthMaterial
	id, err := s.st.Identity.Get()
	if err != nil {
		return m, false, err
	}
	m.Identity = id
	secret := func(c *domain.GitCredential) (domain.Secret, error) {
		stored := find(all, c.ID)
		if stored == nil {
			return "", errNotFound
		}
		cs, err := s.reveal(stored)
		return cs.Secret, err
	}
	if c := res.Push; c != nil {
		v, err := secret(c)
		if err != nil {
			return ports.GitAuthMaterial{Identity: id}, isLocked(err), lockedOrErr(err)
		}
		switch c.Kind {
		case domain.GitHTTPSToken:
			m.HTTPS = &ports.GitHTTPSLogin{Host: c.Host, Username: c.Username, Token: v}
		case domain.GitSSHKey:
			m.SSHKey = v
		}
	}
	if c := res.GitHub; c != nil {
		v, err := secret(c)
		if err != nil {
			return ports.GitAuthMaterial{Identity: id}, isLocked(err), lockedOrErr(err)
		}
		m.GitHubToken = v
	}
	return m, false, nil
}

func isLocked(err error) bool { return errs.Code(err) == domain.CodeGitCredentialStoreLocked }

// lockedOrErr is nil for a locked store (the caller decides what that
// means) and err otherwise.
func lockedOrErr(err error) error {
	if isLocked(err) {
		return nil
	}
	return err
}

var errNoBroker = errors.New("git credentials: no git directories configured")

// Operation opens the credentials of one server-side operation, as stored
// now.
func (s *Service) Operation(_ context.Context, repositoryID, originURL string) (*ports.GitOperation, error) {
	if s.Broker == nil {
		return nil, errNoBroker
	}
	res, all, err := s.resolve(repositoryID, originURL)
	if err != nil {
		return nil, err
	}
	m, locked, err := s.material(res, all)
	switch {
	case err != nil:
		return nil, err
	case locked:
		return nil, domain.ErrGitCredentialStoreLocked
	}
	env, release, err := s.Broker.OpenOperation(func() (ports.GitAuthMaterial, error) { return m, nil })
	if err != nil {
		return nil, err
	}
	return &ports.GitOperation{Env: env, Push: res.Push, GitHub: res.GitHub, GitHubToken: m.GitHubToken, Release: release}, nil
}

// SessionEnv opens the session's git directory, served the repository's
// credentials as they are stored at each request. A locked store or nothing
// resolving still answers: the session runs with no credentials and its
// brief says why.
func (s *Service) SessionEnv(ctx context.Context, sessionID, repositoryID string) (*ports.SessionGit, error) {
	if s.Broker == nil {
		return nil, errNoBroker
	}
	origin := ""
	if r := s.rd.Repositories.Get(ctx, repositoryID); r != nil {
		var err error
		if origin, err = s.origin(ctx, r); err != nil {
			slog.Warn("read origin for a session's git credentials", "session", sessionID, "repository", repositoryID, "err", err)
		}
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	live := liveSession{repositoryID: repositoryID, origin: origin}
	out, err := s.openSession(sessionID, live)
	if err != nil {
		return nil, err
	}
	s.sessions[sessionID] = live
	return out, nil
}

// openSession opens one session's directory; under sessMu.
func (s *Service) openSession(sessionID string, live liveSession) (*ports.SessionGit, error) {
	res, all, err := s.resolve(live.repositoryID, live.origin)
	if err != nil {
		return nil, err
	}
	_, locked, err := s.material(res, all)
	if err != nil {
		return nil, err
	}
	env, envFile, err := s.Broker.OpenSession(sessionID, s.sessionSource(live))
	if err != nil {
		return nil, err
	}
	return &ports.SessionGit{Env: env, EnvFile: envFile, Note: sessionNote(res, locked)}, nil
}

// sessionSource reads live's credentials from the store on every call: a
// locked store answers the identity alone.
func (s *Service) sessionSource(live liveSession) ports.GitAuthSource {
	return func() (ports.GitAuthMaterial, error) {
		res, all, err := s.resolve(live.repositoryID, live.origin)
		if err != nil {
			return ports.GitAuthMaterial{}, err
		}
		m, _, err := s.material(res, all)
		return m, err
	}
}

// sessionNote is the brief's line when the session's git cannot
// authenticate to origin; "" when it can or there is no remote origin.
func sessionNote(res *ports.GitResolution, locked bool) string {
	const tail = " Do not look for other credentials; tell the person, who can fix it on the harness's Git credentials page. Once they have, retry: the session's git reads the stored credentials on every push and fetch, with no restart."
	switch {
	case res.Remote == nil || res.Remote.Kind == "":
		return ""
	case locked:
		return "Git: the harness's credential store is locked (its encryption key is missing), so git push, fetch and gh will fail." + tail
	case res.Push == nil:
		return "Git: no stored credential for " + res.Remote.Host + " (" + string(res.Remote.Kind) + "), so git push and fetch to origin will fail." + tail
	}
	return ""
}

// EndSession closes and removes the session's directory.
func (s *Service) EndSession(sessionID string) {
	if s.Broker == nil {
		return
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	delete(s.sessions, sessionID)
	if err := s.Broker.CloseSession(sessionID); err != nil {
		slog.Warn("remove a session's git directory", "session", sessionID, "err", err)
	}
}

// refreshSessions reopens every live session's directory, so its gitconfig
// carries the identity as it is now. Credentials need no refresh: the
// sockets read them on every request.
func (s *Service) refreshSessions() {
	if s.Broker == nil {
		return
	}
	s.sessMu.Lock()
	defer s.sessMu.Unlock()
	for id, live := range s.sessions {
		if _, err := s.openSession(id, live); err != nil {
			slog.Warn("rewrite a session's git directory", "session", id, "err", err)
		}
	}
}

// LiveSession says whether a session with a git directory is still running,
// and its repository.
type LiveSession func(sessionID string) (repositoryID string, running bool)

// Sweep removes what a previous run left: every operation directory and the
// directory of every session that is not running. The running ones are
// reopened, served from the store from now on.
func (s *Service) Sweep(ctx context.Context, live LiveSession) error {
	if s.Broker == nil {
		return nil
	}
	if err := s.Broker.SweepOperations(); err != nil {
		return err
	}
	ids, err := s.Broker.SessionIDs()
	if err != nil {
		return err
	}
	for _, id := range ids {
		repositoryID, running := live(id)
		if !running {
			s.EndSession(id)
			continue
		}
		if _, err := s.SessionEnv(ctx, id, repositoryID); err != nil {
			slog.Warn("reopen a running session's git directory", "session", id, "err", err)
		}
	}
	return nil
}

// SubscribeSessions removes a session's directory when it ends or is
// deleted.
func (s *Service) SubscribeSessions(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SessionEnded) error {
		s.EndSession(ev.SessionID)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionDeleted) error {
		s.EndSession(ev.SessionID)
		return nil
	})
}

// GitHubToken reads the token for GitHub API calls on the repository, in
// memory only.
func (s *Service) GitHubToken(_ context.Context, repositoryID, originURL string) (*domain.GitCredential, domain.Secret, error) {
	res, all, err := s.resolve(repositoryID, originURL)
	if err != nil || res.GitHub == nil {
		return nil, "", err
	}
	stored := find(all, res.GitHub.ID)
	if stored == nil {
		return nil, "", errNotFound
	}
	cs, err := s.reveal(stored)
	if err != nil {
		return nil, "", err
	}
	return res.GitHub, cs.Secret, nil
}

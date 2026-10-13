// Package gitcredentials keeps the credentials every git remote operation of
// the harness authenticates with: HTTPS tokens and SSH keys, sealed at rest
// with a key held outside the database, the commit identity, and which
// credential a repository is pinned to. It resolves the credential for a
// repository and never answers with the machine's own git setup.
//
// It imports no other application context: it reads repositories and
// projects through narrow store-shaped readers, a checkout's origin through
// ports.OriginReader, and cleans up on ProjectDeleted and RepositoryDeleted.
//
// See "Contract: Harness server ↔ Web UI — git credentials".
package gitcredentials

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitCredentials = (*Service)(nil)

// originReads bounds how many origin URLs a project listing reads at once.
const originReads = 4

// Repositories finds repositories; the sqlite repository store satisfies it.
// Reads go to the store, not the projects service, so a bus handler here
// cannot take a lock the publisher holds.
type Repositories interface {
	Get(id string) *domain.Repository
	ListByProject(projectID string) []*domain.Repository
}

// Projects finds a project; the sqlite project store satisfies it.
type Projects interface {
	Get(id string) *domain.Project
}

// Stores are where the context keeps its state.
type Stores struct {
	Credentials ports.GitCredentialRepository
	Identity    ports.GitIdentityRepository
	Pins        ports.RepositoryCredentialRepository
}

// Readers are what the context reads from elsewhere.
type Readers struct {
	Repositories Repositories
	Projects     Projects
	Origins      ports.OriginReader
}

// Service is the git credentials context.
type Service struct {
	st  Stores
	rd  Readers
	box ports.SecretBox
	// Tester runs test_git_credential; nil answers ok:false.
	Tester ports.GitCredentialTester
	// Files writes the git directories of operations and sessions; nil
	// leaves GitAuth without them (Operation and SessionEnv fail).
	Files ports.GitAuthFiles
	now   func() time.Time

	mu   sync.Mutex // one write at a time
	feed feed

	sessMu   sync.Mutex
	sessions map[string]liveSession // sessions with a git directory
}

// New returns the service. now nil is time.Now.
func New(st Stores, rd Readers, box ports.SecretBox, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, rd: rd, box: box, now: now, feed: newFeed(), sessions: map[string]liveSession{}}
}

// Subscribe drops the pins of deleted repositories and projects, and a
// session's git directory when the session ends or is deleted.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.ProjectDeleted) error {
		return s.st.Pins.DeleteByProject(ev.ProjectID)
	})
	ports.On(sub, func(_ context.Context, ev domain.RepositoryDeleted) error {
		return s.st.Pins.Delete(ev.RepositoryID)
	})
	s.SubscribeSessions(sub)
}

func (s *Service) stamp() time.Time { return domain.GitTime(s.now()) }

func invalid(msg string) error { return domain.GitCredentialError("INVALID_INPUT", msg) }

var errNotFound = domain.GitCredentialError(domain.CodeGitCredentialNotFound, "no such git credential")

func errRepositoryNotFound() error {
	return domain.GitCredentialError("REPOSITORY_NOT_FOUND", "repository not found")
}

// locked reports whether some stored secret was sealed with a key the box
// does not hold (or the box holds none).
func (s *Service) locked(all []*ports.StoredGitCredential) bool {
	id := s.box.KeyID()
	for _, c := range all {
		if c.KeyID != id {
			return true
		}
	}
	return false
}

// seal seals plain, making the key only when nothing is sealed yet.
func (s *Service) seal(all []*ports.StoredGitCredential, plain string) ([]byte, string, error) {
	if s.locked(all) {
		return nil, "", domain.ErrGitCredentialStoreLocked
	}
	return s.box.Seal([]byte(plain), len(all) == 0)
}

func sortCredentials(cs []*domain.GitCredential) {
	slices.SortFunc(cs, func(a, b *domain.GitCredential) int {
		return cmp.Or(cmp.Compare(a.Host, b.Host), cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
}

func publicOf(all []*ports.StoredGitCredential) []*domain.GitCredential {
	out := make([]*domain.GitCredential, 0, len(all))
	for _, c := range all {
		cred := c.Credential
		out = append(out, &cred)
	}
	return out
}

// ListGitCredentials is every credential, sorted by host then name, and
// whether the store is locked, from the key ids alone.
func (s *Service) ListGitCredentials(context.Context) (*ports.GitCredentialList, error) {
	all, err := s.st.Credentials.List()
	if err != nil {
		return nil, err
	}
	return &ports.GitCredentialList{Credentials: s.sorted(all), Locked: s.locked(all)}, nil
}

func (s *Service) sorted(all []*ports.StoredGitCredential) []*domain.GitCredential {
	out := publicOf(all)
	sortCredentials(out)
	return out
}

// credentials is every credential, sorted.
func (s *Service) credentials() ([]*domain.GitCredential, error) {
	all, err := s.st.Credentials.List()
	if err != nil {
		return nil, err
	}
	return s.sorted(all), nil
}

func (s *Service) checkName(all []*ports.StoredGitCredential, name, selfID string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("name is required")
	}
	for _, c := range all {
		if c.Credential.ID != selfID && strings.EqualFold(c.Credential.Name, name) {
			return "", domain.GitCredentialError(domain.CodeGitCredentialNameTaken, "another credential is named "+c.Credential.Name)
		}
	}
	return name, nil
}

func checkHost(raw string) (string, error) {
	h := domain.NormalizeGitHost(raw)
	if h == "" {
		return "", invalid("host is required")
	}
	return h, nil
}

// clearDefault clears is_default on every other credential of (host, kind).
func (s *Service) clearDefault(all []*ports.StoredGitCredential, host string, kind domain.GitCredentialKind, selfID string) error {
	for _, c := range all {
		if c.Credential.ID == selfID || !c.Credential.IsDefault || c.Credential.Host != host || c.Credential.Kind != kind {
			continue
		}
		c.Credential.IsDefault = false
		c.Credential.UpdatedAt = s.stamp()
		if err := s.st.Credentials.Update(c); err != nil {
			return err
		}
		cred := c.Credential
		s.feed.announce(ports.GitCredentialChange{Credential: &cred})
	}
	return nil
}

// CreateGitCredential stores a new credential, sealing its secret.
func (s *Service) CreateGitCredential(_ context.Context, in ports.CreateGitCredential) (*domain.GitCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.st.Credentials.List()
	if err != nil {
		return nil, err
	}
	name, err := s.checkName(all, in.Name, "")
	if err != nil {
		return nil, err
	}
	if !in.Kind.Valid() {
		return nil, invalid(`kind must be "https_token" or "ssh_key"`)
	}
	host, err := checkHost(in.Host)
	if err != nil {
		return nil, err
	}
	now := s.stamp()
	c := domain.GitCredential{
		Name: name, Kind: in.Kind, Host: host, Username: strings.TrimSpace(in.Username),
		IsDefault: in.IsDefault, CreatedAt: now, UpdatedAt: now,
	}
	if c.Username == "" {
		c.Username = in.Kind.DefaultUsername()
	}
	var secret string
	switch in.Kind {
	case domain.GitHTTPSToken:
		secret = strings.TrimSpace(in.Token.Reveal())
		if secret == "" {
			return nil, invalid("token is required for an https_token")
		}
		c.Fingerprint = tokenFingerprint(secret)
	case domain.GitSSHKey:
		var key *sshKey
		switch {
		case in.Generate && in.PrivateKey != "":
			return nil, invalid("give either private_key or generate, not both")
		case in.Generate:
			key, err = generateSSHKey()
		case in.PrivateKey != "":
			key, err = parseSSHKey(in.PrivateKey.Reveal(), in.Passphrase.Reveal())
		default:
			return nil, invalid("an ssh_key needs private_key or generate: true")
		}
		if err != nil {
			return nil, err
		}
		secret, c.Fingerprint, c.PublicKey = string(key.pem), key.fingerprint, &key.publicKey
	}
	sealed, keyID, err := s.seal(all, secret)
	if err != nil {
		return nil, err
	}
	if c.IsDefault {
		if err := s.clearDefault(all, c.Host, c.Kind, ""); err != nil {
			return nil, err
		}
	}
	stored, err := s.st.Credentials.Create(&ports.StoredGitCredential{Credential: c, Sealed: sealed, KeyID: keyID})
	if err != nil {
		return nil, err
	}
	out := stored.Credential
	s.feed.announce(ports.GitCredentialChange{Credential: &out})
	s.refreshSessions()
	return &out, nil
}

func find(all []*ports.StoredGitCredential, id string) *ports.StoredGitCredential {
	for _, c := range all {
		if c.Credential.ID == id {
			return c
		}
	}
	return nil
}

// UpdateGitCredential changes what in sets. A host change on a credential a
// repository is pinned to is refused; a new secret is sealed.
func (s *Service) UpdateGitCredential(ctx context.Context, in ports.UpdateGitCredential) (*domain.GitCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.st.Credentials.List()
	if err != nil {
		return nil, err
	}
	cur := find(all, in.CredentialID)
	if cur == nil {
		return nil, errNotFound
	}
	c := cur.Credential
	if in.Name != nil {
		if c.Name, err = s.checkName(all, *in.Name, c.ID); err != nil {
			return nil, err
		}
	}
	if in.Host != nil {
		host, err := checkHost(*in.Host)
		if err != nil {
			return nil, err
		}
		if host != c.Host {
			if err := s.refuseHostChange(c.ID); err != nil {
				return nil, err
			}
			c.Host = host
		}
	}
	if in.Username != nil {
		c.Username = strings.TrimSpace(*in.Username)
		if c.Username == "" {
			c.Username = c.Kind.DefaultUsername()
		}
	}
	sealed, keyID := cur.Sealed, cur.KeyID
	if in.CarriesSecret() {
		var secret string
		switch {
		case c.Kind == domain.GitHTTPSToken && in.PrivateKey == "":
			secret = strings.TrimSpace(in.Token.Reveal())
			c.Fingerprint = tokenFingerprint(secret)
		case c.Kind == domain.GitSSHKey && in.Token == "":
			key, err := parseSSHKey(in.PrivateKey.Reveal(), in.Passphrase.Reveal())
			if err != nil {
				return nil, err
			}
			secret, c.Fingerprint, c.PublicKey = string(key.pem), key.fingerprint, &key.publicKey
		case c.Kind == domain.GitHTTPSToken:
			return nil, invalid("an https_token takes a token, not a private_key")
		default:
			return nil, invalid("an ssh_key takes a private_key, not a token")
		}
		if sealed, keyID, err = s.seal(all, secret); err != nil {
			return nil, err
		}
	}
	if in.IsDefault != nil {
		c.IsDefault = *in.IsDefault
	}
	if c.IsDefault {
		if err := s.clearDefault(all, c.Host, c.Kind, c.ID); err != nil {
			return nil, err
		}
	}
	c.UpdatedAt = s.stamp()
	if err := s.st.Credentials.Update(&ports.StoredGitCredential{Credential: c, Sealed: sealed, KeyID: keyID}); err != nil {
		return nil, err
	}
	s.feed.announce(ports.GitCredentialChange{Credential: &c})
	s.refreshSessions()
	return &c, nil
}

// refuseHostChange is GIT_CREDENTIAL_HOST_MISMATCH naming the repositories
// pinned to credentialID, nil when there are none.
func (s *Service) refuseHostChange(credentialID string) error {
	pins, err := s.st.Pins.ListByCredential(credentialID)
	if err != nil || len(pins) == 0 {
		return err
	}
	repos := make([]map[string]string, 0, len(pins))
	for _, p := range pins {
		name := p.RepositoryID
		if r := s.rd.Repositories.Get(p.RepositoryID); r != nil {
			name = r.Name
		}
		repos = append(repos, map[string]string{"id": p.RepositoryID, "name": name})
	}
	return &domain.DetailedError{
		StructuredError: domain.GitCredentialError(domain.CodeGitCredentialHostMismatch,
			"repositories are pinned to this credential; unpin them before changing its host"),
		Details: map[string]any{"repositories": repos},
	}
}

// DeleteGitCredential removes the credential; repositories pinned to it
// resolve automatically again.
func (s *Service) DeleteGitCredential(_ context.Context, credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.st.Credentials.Get(credentialID)
	if err != nil {
		return err
	}
	if cur == nil {
		return errNotFound
	}
	if err := s.st.Pins.DeleteByCredential(credentialID); err != nil {
		return err
	}
	if err := s.st.Credentials.Delete(credentialID); err != nil {
		return err
	}
	s.feed.announce(ports.GitCredentialChange{Credential: &domain.GitCredential{ID: credentialID}, Deleted: true})
	s.refreshSessions()
	return nil
}

// reveal is c with its secret, or GIT_CREDENTIAL_STORE_LOCKED.
func (s *Service) reveal(c *ports.StoredGitCredential) (ports.GitCredentialSecret, error) {
	if c.KeyID != s.box.KeyID() {
		return ports.GitCredentialSecret{}, domain.ErrGitCredentialStoreLocked
	}
	plain, err := s.box.Open(c.Sealed)
	if err != nil {
		return ports.GitCredentialSecret{}, err
	}
	return ports.GitCredentialSecret{GitCredential: c.Credential, Secret: domain.Secret(plain)}, nil
}

// TestGitCredential checks the credential against the repository's origin,
// or its host. A host refusal is ok:false; success records last_used_at.
func (s *Service) TestGitCredential(ctx context.Context, credentialID, repositoryID string) (*ports.GitCredentialTest, error) {
	cur, err := s.st.Credentials.Get(credentialID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, errNotFound
	}
	remote := ""
	if repositoryID != "" {
		r := s.rd.Repositories.Get(repositoryID)
		if r == nil {
			return nil, errRepositoryNotFound()
		}
		if remote, err = s.origin(ctx, r); err != nil {
			return nil, err
		}
		if remote == "" {
			return &ports.GitCredentialTest{OK: false, Message: "the repository has no origin remote"}, nil
		}
	}
	cred, err := s.reveal(cur)
	if err != nil {
		return nil, err
	}
	if s.Tester == nil {
		return &ports.GitCredentialTest{OK: false, Message: "testing credentials is not available on this server"}, nil
	}
	res, err := s.Tester.Test(ctx, cred, remote)
	if err != nil {
		return nil, err
	}
	res.CheckedURL = domain.StripURLUserinfo(res.CheckedURL)
	if res.OK {
		s.Used(credentialID)
	}
	return res, nil
}

// Used records that the credential just authenticated, and announces it.
func (s *Service) Used(credentialID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.st.Credentials.Get(credentialID)
	if err != nil || cur == nil {
		return
	}
	at := s.stamp()
	cur.Credential.LastUsedAt = &at
	if s.st.Credentials.Update(cur) == nil {
		c := cur.Credential
		s.feed.announce(ports.GitCredentialChange{Credential: &c})
	}
}

// GetGitIdentity is the identity, nil while none is set.
func (s *Service) GetGitIdentity(context.Context) (*domain.GitIdentity, error) {
	return s.st.Identity.Get()
}

// UpdateGitIdentity sets the commit identity.
func (s *Service) UpdateGitIdentity(_ context.Context, name, email string) (*domain.GitIdentity, error) {
	id := domain.GitIdentity{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)}
	switch {
	case id.Name == "":
		return nil, invalid("name is required")
	case id.Email == "":
		return nil, invalid("email is required")
	case strings.Count(id.Email, "@") != 1 || strings.HasPrefix(id.Email, "@") || strings.HasSuffix(id.Email, "@"):
		return nil, invalid("email must be one address, like you@example.com")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.st.Identity.Put(id); err != nil {
		return nil, err
	}
	s.feed.announce(ports.GitCredentialChange{Identity: &id})
	s.refreshSessions()
	return &id, nil
}

// origin is the repository's origin URL, "" when it has none.
func (s *Service) origin(ctx context.Context, r *domain.Repository) (string, error) {
	if r.RootDir == "" || s.rd.Origins == nil {
		return "", nil
	}
	return s.rd.Origins.OriginURL(ctx, r.RootDir)
}

// repositoryCredential is r's binding, what it resolves to now and its origin.
func (s *Service) repositoryCredential(ctx context.Context, r *domain.Repository, creds []*domain.GitCredential) (*domain.RepositoryCredential, error) {
	pinned, err := s.st.Pins.Get(r.ID)
	if err != nil {
		return nil, err
	}
	origin, err := s.origin(ctx, r)
	if err != nil {
		return nil, err
	}
	out := &domain.RepositoryCredential{Binding: domain.RepositoryCredentialBinding{RepositoryID: r.ID}}
	if pinned != "" {
		out.Binding.CredentialID = &pinned
	}
	var remote *domain.GitRemoteURL
	if origin != "" {
		shown := domain.StripURLUserinfo(origin)
		out.RemoteURL = &shown
		if u, ok := domain.ParseGitRemoteURL(origin); ok {
			remote = &u
		}
	}
	out.Resolved = domain.ResolveGitCredential(creds, pinned, remote)
	return out, nil
}

// GetRepositoryCredential is the repository's binding and what it resolves to.
func (s *Service) GetRepositoryCredential(ctx context.Context, repositoryID string) (*domain.RepositoryCredential, error) {
	r := s.rd.Repositories.Get(repositoryID)
	if r == nil {
		return nil, errRepositoryNotFound()
	}
	creds, err := s.credentials()
	if err != nil {
		return nil, err
	}
	return s.repositoryCredential(ctx, r, creds)
}

// ListRepositoryCredentials answers for each of the project's repositories,
// in its order, reading their origins a few at a time.
func (s *Service) ListRepositoryCredentials(ctx context.Context, projectID string) ([]*domain.RepositoryCredential, error) {
	if s.rd.Projects.Get(projectID) == nil {
		return nil, domain.GitCredentialError("PROJECT_NOT_FOUND", "project not found")
	}
	creds, err := s.credentials()
	if err != nil {
		return nil, err
	}
	repos := s.rd.Repositories.ListByProject(projectID)
	out := make([]*domain.RepositoryCredential, len(repos))
	errs := make([]error, len(repos))
	sem := make(chan struct{}, originReads)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			out[i], errs[i] = s.repositoryCredential(ctx, r, creds)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetRepositoryCredential pins the repository to credentialID, or unpins it
// with nil. A pin must fit the origin's host and scheme; a repository with no
// origin can be pinned.
func (s *Service) SetRepositoryCredential(ctx context.Context, repositoryID string, credentialID *string) (*domain.RepositoryCredential, error) {
	r := s.rd.Repositories.Get(repositoryID)
	if r == nil {
		return nil, errRepositoryNotFound()
	}
	if credentialID == nil || *credentialID == "" {
		s.mu.Lock()
		err := s.st.Pins.Delete(repositoryID)
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		s.refreshSessions()
		return s.GetRepositoryCredential(ctx, repositoryID)
	}
	origin, err := s.origin(ctx, r)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	cur, err := s.st.Credentials.Get(*credentialID)
	if err == nil && cur == nil {
		err = errNotFound
	}
	if err == nil {
		if u, ok := domain.ParseGitRemoteURL(origin); ok {
			err = checkPin(cur.Credential, u)
		}
	}
	if err == nil {
		err = s.st.Pins.Put(ports.RepositoryCredentialPin{RepositoryID: r.ID, ProjectID: r.ProjectID, CredentialID: *credentialID})
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.refreshSessions()
	return s.GetRepositoryCredential(ctx, repositoryID)
}

func checkPin(c domain.GitCredential, origin domain.GitRemoteURL) error {
	switch {
	case c.Host != origin.Host:
		return domain.GitCredentialError(domain.CodeGitCredentialHostMismatch,
			"the credential is for "+c.Host+" but origin is on "+origin.Host)
	case origin.Kind != c.Kind:
		want := "an https_token"
		if origin.Kind == domain.GitSSHKey {
			want = "an ssh_key"
		} else if origin.Kind == "" {
			want = "no credential"
		}
		return domain.GitCredentialError(domain.CodeGitCredentialKindMismatch, "origin takes "+want+", not an "+string(c.Kind))
	}
	return nil
}

// FollowGitCredentials streams every change until ctx ends.
func (s *Service) FollowGitCredentials(ctx context.Context) (<-chan ports.GitCredentialChange, error) {
	return s.feed.follow(ctx), nil
}

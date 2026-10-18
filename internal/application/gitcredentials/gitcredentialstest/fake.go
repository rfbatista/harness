// Package gitcredentialstest is the git credentials context in memory: the
// real service over in-memory stores, an in-memory key and a repository
// catalog you fill by hand. No git, no files. Page tests in
// internal/adapter/in/web bind it as web.Deps.GitCredentials:
//
//	fake := gitcredentialstest.New()
//	projectID, repoID := fake.AddRepository("", "widgets", "https://github.com/acme/widgets.git")
//	fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "GitHub", Kind: domain.GitHTTPSToken,
//		Host: "github.com", Token: "ghp_example"})
//	fake.Lock() // from now on the store is locked (GIT_CREDENTIAL_STORE_LOCKED)
//
// It passes the same contract suite (portstest.GitCredentialsConformance) as
// the service over SQLite and the HTTP client.
package gitcredentialstest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Fake is the service over memory. It satisfies ports.GitCredentials and
// ports.GitAuth; Broker serves what its git directories would.
type Fake struct {
	*gitcredentials.Service
	Catalog *Catalog
	Broker  *Broker
	box     *Box
}

// New returns an empty fake.
func New() *Fake {
	cat := NewCatalog()
	box := NewBox()
	svc := gitcredentials.New(
		gitcredentials.Stores{Credentials: NewCredentials(), Identity: &Identity{}, Pins: NewPins()},
		gitcredentials.Readers{Repositories: cat, Projects: cat.Projects(), Origins: cat},
		box, nil)
	broker := NewBroker()
	svc.Broker = broker
	return &Fake{Service: svc, Catalog: cat, Broker: broker, box: box}
}

// AddRepository records a repository whose origin is originURL ("" for none)
// in projectID, or in a new project when projectID is "".
func (f *Fake) AddRepository(projectID, name, originURL string) (string, string) {
	return f.Catalog.Add(projectID, name, originURL)
}

// Lock takes the key away: whatever is sealed can no longer be opened.
func (f *Fake) Lock() { f.box.Lock() }

var seq atomic.Int64

func nextID(prefix string) string { return fmt.Sprintf("%s%d", prefix, seq.Add(1)) }

// Catalog is projects and repositories with their origins, in memory. It
// satisfies gitcredentials.Repositories, gitcredentials.Projects and
// ports.OriginReader (the origin is keyed by the repository's root dir).
type Catalog struct {
	mu       sync.Mutex
	projects map[string]*domain.Project
	repos    []*domain.Repository
	origins  map[string]string // root dir → origin
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{projects: map[string]*domain.Project{}, origins: map[string]string{}}
}

// Add records a repository, and its project when projectID is "".
func (c *Catalog) Add(projectID, name, originURL string) (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if projectID == "" {
		projectID = nextID("project-")
	}
	if c.projects[projectID] == nil {
		c.projects[projectID] = &domain.Project{ID: projectID, Name: projectID}
	}
	r := &domain.Repository{ID: nextID("repo-"), ProjectID: projectID, Name: name, URL: originURL, RootDir: "/fake/" + nextID("checkout-")}
	c.repos = append(c.repos, r)
	c.origins[r.RootDir] = originURL
	return projectID, r.ID
}

// SetOrigin changes a repository's origin; "" removes it.
func (c *Catalog) SetOrigin(repositoryID, originURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.repos {
		if r.ID == repositoryID {
			c.origins[r.RootDir] = originURL
		}
	}
}

func (c *Catalog) Get(id string) *domain.Repository {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.repos {
		if r.ID == id {
			cp := *r
			return &cp
		}
	}
	return nil
}

func (c *Catalog) ListByProject(projectID string) []*domain.Repository {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []*domain.Repository{}
	for _, r := range c.repos {
		if r.ProjectID == projectID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}

// projectGetter answers gitcredentials.Projects; Catalog cannot have two Get
// methods.
type projectGetter struct{ c *Catalog }

func (p projectGetter) Get(id string) *domain.Project {
	p.c.mu.Lock()
	defer p.c.mu.Unlock()
	return p.c.projects[id]
}

// Projects is the catalog as gitcredentials.Projects.
func (c *Catalog) Projects() gitcredentials.Projects { return projectGetter{c} }

func (c *Catalog) OriginURL(_ context.Context, root string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.origins[root], nil
}

// Box is ports.SecretBox in memory. It does not encrypt: nothing it seals
// leaves the process.
type Box struct {
	mu     sync.Mutex
	id     string
	locked bool
}

// NewBox returns a box with no key yet.
func NewBox() *Box { return &Box{} }

// Lock drops the key.
func (b *Box) Lock() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.id, b.locked = "", true
}

func (b *Box) KeyID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id
}

func (b *Box) Seal(plain []byte, create bool) ([]byte, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.id == "" {
		if !create || b.locked {
			return nil, "", domain.ErrGitCredentialStoreLocked
		}
		var k [16]byte
		_, _ = rand.Read(k[:])
		sum := sha256.Sum256(k[:])
		b.id = hex.EncodeToString(sum[:8])
	}
	return slices.Clone(plain), b.id, nil
}

func (b *Box) Open(sealed []byte) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.id == "" {
		return nil, domain.ErrGitCredentialStoreLocked
	}
	return slices.Clone(sealed), nil
}

// Credentials is ports.GitCredentialRepository in memory.
type Credentials struct {
	mu   sync.Mutex
	rows []*ports.StoredGitCredential
}

// NewCredentials returns an empty store.
func NewCredentials() *Credentials { return &Credentials{} }

func clone(c *ports.StoredGitCredential) *ports.StoredGitCredential {
	cp := *c
	cp.Sealed = slices.Clone(c.Sealed)
	if c.Credential.PublicKey != nil {
		pk := *c.Credential.PublicKey
		cp.Credential.PublicKey = &pk
	}
	if c.Credential.LastUsedAt != nil {
		at := *c.Credential.LastUsedAt
		cp.Credential.LastUsedAt = &at
	}
	return &cp
}

func (s *Credentials) List() ([]*ports.StoredGitCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ports.StoredGitCredential, 0, len(s.rows))
	for _, c := range s.rows {
		out = append(out, clone(c))
	}
	return out, nil
}

func (s *Credentials) Get(id string) (*ports.StoredGitCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.rows {
		if c.Credential.ID == id {
			return clone(c), nil
		}
	}
	return nil, nil
}

func (s *Credentials) Create(c *ports.StoredGitCredential) (*ports.StoredGitCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := clone(c)
	row.Credential.ID = nextID("cred-")
	s.rows = append(s.rows, row)
	return clone(row), nil
}

func (s *Credentials) Update(c *ports.StoredGitCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, row := range s.rows {
		if row.Credential.ID == c.Credential.ID {
			s.rows[i] = clone(c)
			return nil
		}
	}
	return fmt.Errorf("git credential %s not found", c.Credential.ID)
}

func (s *Credentials) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = slices.DeleteFunc(s.rows, func(c *ports.StoredGitCredential) bool { return c.Credential.ID == id })
	return nil
}

// Identity is ports.GitIdentityRepository in memory.
type Identity struct {
	mu sync.Mutex
	id *domain.GitIdentity
}

func (s *Identity) Get() (*domain.GitIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id == nil {
		return nil, nil
	}
	cp := *s.id
	return &cp, nil
}

func (s *Identity) Put(id domain.GitIdentity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = &id
	return nil
}

// Pins is ports.RepositoryCredentialRepository in memory.
type Pins struct {
	mu   sync.Mutex
	pins map[string]ports.RepositoryCredentialPin
}

// NewPins returns an empty store.
func NewPins() *Pins { return &Pins{pins: map[string]ports.RepositoryCredentialPin{}} }

func (s *Pins) Get(repositoryID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pins[repositoryID].CredentialID, nil
}

func (s *Pins) ListByRepositories(repositoryIDs []string) ([]ports.RepositoryCredentialPin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.RepositoryCredentialPin
	for _, id := range repositoryIDs {
		if p, ok := s.pins[id]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Pins) Put(pin ports.RepositoryCredentialPin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pins[pin.RepositoryID] = pin
	return nil
}

func (s *Pins) Delete(repositoryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pins, repositoryID)
	return nil
}

func (s *Pins) ListByCredential(credentialID string) ([]ports.RepositoryCredentialPin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.RepositoryCredentialPin
	for _, p := range s.pins {
		if p.CredentialID == credentialID {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b ports.RepositoryCredentialPin) int {
		switch {
		case a.RepositoryID < b.RepositoryID:
			return -1
		case a.RepositoryID > b.RepositoryID:
			return 1
		}
		return 0
	})
	return out, nil
}

func (s *Pins) DeleteByCredential(credentialID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.pins {
		if p.CredentialID == credentialID {
			delete(s.pins, id)
		}
	}
	return nil
}

func (s *Pins) DeleteByProject(projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, p := range s.pins {
		if p.ProjectID == projectID {
			delete(s.pins, id)
		}
	}
	return nil
}

// Broker is ports.GitAuthBroker in memory: it keeps each directory's source
// and answers an environment naming it, so a test can see which credentials
// an operation or a session would be served.
type Broker struct {
	mu       sync.Mutex
	sessions map[string]ports.GitAuthSource
	ops      map[string]ports.GitAuthSource
}

// NewBroker returns an empty one.
func NewBroker() *Broker {
	return &Broker{sessions: map[string]ports.GitAuthSource{}, ops: map[string]ports.GitAuthSource{}}
}

// EnvKey is the variable the environments carry: the directory's name.
const EnvKey = "HARNESS_TEST_GIT_DIR"

func (b *Broker) OpenSession(sessionID string, src ports.GitAuthSource) ([]string, string, error) {
	if _, err := src(); err != nil {
		return nil, "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[sessionID] = src
	return []string{EnvKey + "=session-" + sessionID}, "/fake/git/sessions/" + sessionID + "/env.sh", nil
}

func (b *Broker) CloseSession(sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
	return nil
}

func (b *Broker) SessionIDs() ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ids []string
	for id := range b.sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

func (b *Broker) OpenOperation(src ports.GitAuthSource) ([]string, func(), error) {
	if _, err := src(); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	id := nextID("op-")
	b.ops[id] = src
	return []string{EnvKey + "=" + id}, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.ops, id)
	}, nil
}

func (b *Broker) SweepOperations() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops = map[string]ports.GitAuthSource{}
	return nil
}

// Session is what the session's directory would be served now, and
// whether it has one.
func (b *Broker) Session(sessionID string) (ports.GitAuthMaterial, bool) {
	b.mu.Lock()
	src, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if !ok {
		return ports.GitAuthMaterial{}, false
	}
	m, err := src()
	if err != nil {
		return ports.GitAuthMaterial{}, false
	}
	return m, true
}

// Operations counts the operation directories not yet released.
func (b *Broker) Operations() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ops)
}

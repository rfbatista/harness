package gitcredentialstest

import (
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/gitcredentials"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// SQLite is the service as the server builds it: SQLite stores, a key file
// under a temporary data dir, and real git checkouts for origins.
type SQLite struct {
	Service  *gitcredentials.Service
	DB       *gorm.DB
	Projects *sqlite.ProjectRepository
	Repos    *sqlite.RepositoryRepository
	Box      *Swap
	Dir      string
}

// NewSQLite builds it under t.TempDir().
func NewSQLite(t *testing.T) *SQLite {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	box, err := secretbox.New("", filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	env := &SQLite{
		DB: db, Projects: sqlite.NewProjectRepository(db), Repos: sqlite.NewRepositoryRepository(db),
		Box: &Swap{inner: box}, Dir: dir,
	}
	env.Service = gitcredentials.New(
		gitcredentials.Stores{
			Credentials: sqlite.NewGitCredentialRepository(db),
			Identity:    sqlite.NewGitIdentityRepository(db),
			Pins:        sqlite.NewRepositoryCredentialRepository(db),
		},
		gitcredentials.Readers{Repositories: env.Repos, Projects: env.Projects, Origins: gitcli.NewBranchPublisher()},
		env.Box, nil)
	return env
}

// AddRepository makes a git checkout whose origin is originURL ("" for none)
// and records it in projectID, or in a new project when projectID is "".
func (e *SQLite) AddRepository(t *testing.T, projectID, name, originURL string) (string, string) {
	t.Helper()
	if projectID == "" {
		p, err := e.Projects.Create(name+"-project-"+filepath.Base(t.TempDir()), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		projectID = p.ID
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if originURL != "" {
		run("remote", "add", "origin", originURL)
	}
	url := originURL
	if url == "" {
		url = "file://" + root
	}
	r, err := e.Repos.Create(projectID, name, "", url, root)
	if err != nil {
		t.Fatal(err)
	}
	return projectID, r.ID
}

// Lock swaps in a box over a key file that does not exist: what is sealed
// can no longer be read.
func (e *SQLite) Lock(t *testing.T) {
	t.Helper()
	box, err := secretbox.New("", filepath.Join(t.TempDir(), "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	e.Box.Set(box)
}

// Fixture is the contract suite's fixture over this environment.
func (e *SQLite) Fixture() portstest.GitCredentialsFixture {
	return portstest.GitCredentialsFixture{AddRepository: e.AddRepository, Lock: e.Lock}
}

// Swap is a ports.SecretBox whose box can be replaced, to lock a store in a
// test.
type Swap struct {
	mu    sync.Mutex
	inner ports.SecretBox
}

// Set replaces the box.
func (s *Swap) Set(b ports.SecretBox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner = b
}

func (s *Swap) box() ports.SecretBox {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inner
}

func (s *Swap) KeyID() string { return s.box().KeyID() }

func (s *Swap) Seal(plain []byte, create bool) ([]byte, string, error) {
	return s.box().Seal(plain, create)
}

func (s *Swap) Open(sealed []byte) ([]byte, error) { return s.box().Open(sealed) }

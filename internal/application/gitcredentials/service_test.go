package gitcredentials_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/secretbox"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/application/projects"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var ctx = context.Background()

func githubToken(t *testing.T, p ports.GitCredentials, name string) *domain.GitCredential {
	t.Helper()
	c, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: name, Kind: domain.GitHTTPSToken, Host: "github.com", Token: "ghp_servicetest0001"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Deleting a repository or its project drops its pin, through the bus the
// projects context publishes on.
func TestPinsGoWithTheirRepositoryAndProject(t *testing.T) {
	env := gitcredentialstest.NewSQLite(t)
	bus := eventbus.New()
	env.Service.Subscribe(bus)
	catalog := projects.NewService(env.Projects, env.Repos, bus)
	c := githubToken(t, env.Service, "GitHub")

	project, kept := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	_, dropped := env.AddRepository(t, project, "web", "https://github.com/acme/web.git")
	other, elsewhere := env.AddRepository(t, "", "other", "https://github.com/acme/other.git")
	for _, r := range []string{kept, dropped, elsewhere} {
		if _, err := env.Service.SetRepositoryCredential(ctx, r, &c.ID); err != nil {
			t.Fatal(err)
		}
	}
	pins := sqliteRepositoryCredentials(env)

	if err := catalog.DeleteRepository(ctx, dropped); err != nil {
		t.Fatal(err)
	}
	if id, _ := pins.Get(dropped); id != "" {
		t.Fatal("a deleted repository kept its pin")
	}
	if id, _ := pins.Get(kept); id != c.ID {
		t.Fatal("deleting one repository dropped another's pin")
	}
	if err := catalog.DeleteProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if id, _ := pins.Get(kept); id != "" {
		t.Fatal("a deleted project's repository kept its pin")
	}
	if id, _ := pins.Get(elsewhere); id != c.ID {
		t.Fatalf("project %s's pin went with another project", other)
	}
}

// The key file appears with the first secret, owner-only, and a store whose
// key is gone or replaced is locked, never wiped or re-keyed.
func TestTheKeyFileIsMadeOnceAndALostKeyLocks(t *testing.T) {
	env := gitcredentialstest.NewSQLite(t)
	keyFile := filepath.Join(env.Dir, "secret.key")
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatalf("a key file exists before any secret: %v", err)
	}
	if _, err := env.Service.UpdateGitIdentity(ctx, "Ada", "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatal("setting the identity made a key")
	}
	c := githubToken(t, env.Service, "GitHub")
	info, err := os.Stat(keyFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file after the first secret: %v, %v", info, err)
	}

	// The server restarts with the key file gone: locked, and it stays so
	// even though a new key could be made.
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	box, err := secretbox.New("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	env.Box.Set(box)
	_, err = env.Service.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "other", Kind: domain.GitHTTPSToken, Host: "gitlab.com", Token: "glpat_x"})
	if errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("create with the key gone: %v, want STORE_LOCKED", err)
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Fatal("a locked store made a new key over sealed secrets")
	}
	// An environment key that is not the one that sealed them locks it too.
	envKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	wrong, err := secretbox.New(envKey, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	env.Box.Set(wrong)
	if _, err := env.Service.TestGitCredential(ctx, c.ID, ""); errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("test with the wrong key: %v, want STORE_LOCKED", err)
	}
	if list, err := env.Service.ListGitCredentials(ctx); err != nil || len(list.Credentials) != 1 || !list.Locked {
		t.Fatalf("the credential is gone or unreadable while locked: %v, %v", list, err)
	}
}

// The token and the private key are sealed: the database holds neither in
// clear.
func TestSecretsAreSealedInTheDatabase(t *testing.T) {
	env := gitcredentialstest.NewSQLite(t)
	githubToken(t, env.Service, "GitHub")
	if _, err := env.Service.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true}); err != nil {
		t.Fatal(err)
	}
	if err := sqliteClose(env); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"ghp_servicetest0001", "OPENSSH PRIVATE KEY"} {
		if containsBytes(raw, s) {
			t.Fatalf("the database holds %q in clear", s)
		}
	}
}

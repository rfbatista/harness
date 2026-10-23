package portstest

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// GitCredentialsFixture is what the git credentials suite needs from the side
// it runs against.
type GitCredentialsFixture struct {
	// AddRepository records a repository whose origin is originURL ("" for
	// none) in projectID, or in a new project when projectID is "", and
	// answers both ids.
	AddRepository func(t *testing.T, projectID, name, originURL string) (string, string)
	// Lock takes the store's key away: what is sealed can no longer be read.
	Lock func(t *testing.T)
}

// Secrets the suite stores; nothing it reads back may contain them.
const (
	testToken      = "ghp_conformanceTokenValue0001"
	otherToken     = "ghp_conformanceTokenValue0002"
	testPassphrase = "correct horse battery staple"
)

// encryptedTestKey is a fresh ed25519 key in OpenSSH PEM, encrypted with
// testPassphrase.
func encryptedTestKey(t *testing.T) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(testPassphrase))
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

// noSecrets fails t when v, as JSON, carries a stored secret.
func noSecrets(t *testing.T, v any, secrets ...string) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append([]string{testToken, otherToken, testPassphrase, "PRIVATE KEY"}, secrets...) {
		if strings.Contains(string(b), s) {
			t.Fatalf("a secret is visible: %s", b)
		}
	}
}

// detailsOf is a coded error's details as JSON values, nil when it has none.
func detailsOf(t *testing.T, err error) map[string]any {
	t.Helper()
	var de *domain.DetailedError
	if !errors.As(err, &de) || de.Details == nil {
		return nil
	}
	b, err := json.Marshal(de.Details)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func ptr[T any](v T) *T { return &v }

// GitCredentialsConformance runs the git credentials contract against a
// fresh, empty ports.GitCredentials from newPort.
func GitCredentialsConformance(t *testing.T, newPort func(t *testing.T) (ports.GitCredentials, GitCredentialsFixture)) {
	ctx := context.Background()

	token := func(t *testing.T, p ports.GitCredentials, name, host string, isDefault bool) *domain.GitCredential {
		t.Helper()
		c, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: name, Kind: domain.GitHTTPSToken, Host: host, Token: testToken, IsDefault: isDefault})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	generated := func(t *testing.T, p ports.GitCredentials, name, host string) *domain.GitCredential {
		t.Helper()
		c, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: name, Kind: domain.GitSSHKey, Host: host, Generate: true})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	t.Run("an empty store lists nothing and has no identity", func(t *testing.T) {
		p, _ := newPort(t)
		list, err := p.ListGitCredentials(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if list == nil || list.Credentials == nil || len(list.Credentials) != 0 || list.Locked {
			t.Fatalf("ListGitCredentials = %#v, want an empty, non-nil list, not locked", list)
		}
		id, err := p.GetGitIdentity(ctx)
		if err != nil || id != nil {
			t.Fatalf("GetGitIdentity = %+v, %v; want nil, nil", id, err)
		}
	})

	t.Run("an https token round trip shows only its fingerprint", func(t *testing.T) {
		p, _ := newPort(t)
		before := time.Now().Add(-time.Second)
		c, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{
			Name: "  GitHub  ", Kind: domain.GitHTTPSToken, Host: "https://GitHub.com/acme", Token: testToken,
		})
		if err != nil {
			t.Fatal(err)
		}
		if c.ID == "" || c.Name != "GitHub" || c.Kind != domain.GitHTTPSToken || c.Host != "github.com" ||
			c.Username != "x-access-token" || c.Fingerprint != "…0001" || c.PublicKey != nil || c.IsDefault || c.LastUsedAt != nil {
			t.Fatalf("created %+v", c)
		}
		if c.CreatedAt.Before(before) || c.CreatedAt.Location() != time.UTC || c.CreatedAt.Nanosecond() != 0 || !c.UpdatedAt.Equal(c.CreatedAt) {
			t.Fatalf("times %v / %v: want UTC whole seconds, now", c.CreatedAt, c.UpdatedAt)
		}
		list, err := p.ListGitCredentials(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(list.Credentials) != 1 || list.Credentials[0].ID != c.ID || list.Locked {
			t.Fatalf("ListGitCredentials = %+v", list)
		}
		a, _ := json.Marshal(list.Credentials[0])
		b, _ := json.Marshal(c)
		if string(a) != string(b) {
			t.Fatalf("listed %s, created %s", a, b)
		}
		noSecrets(t, list)
		noSecrets(t, c)
	})

	t.Run("ssh keys: generated, imported with a passphrase, refused when they do not parse", func(t *testing.T) {
		p, _ := newPort(t)
		g := generated(t, p, "deploy", "github.com")
		if g.Username != "git" || g.PublicKey == nil || !strings.HasPrefix(*g.PublicKey, "ssh-ed25519 ") ||
			!strings.HasPrefix(g.Fingerprint, "SHA256:") {
			t.Fatalf("generated %+v", g)
		}
		key := encryptedTestKey(t)
		_, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "laptop", Kind: domain.GitSSHKey, Host: "github.com", PrivateKey: domain.Secret(key)})
		wantCode(t, err, domain.CodeGitCredentialInvalidKey)
		_, err = p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "laptop", Kind: domain.GitSSHKey, Host: "github.com", PrivateKey: domain.Secret(key), Passphrase: "wrong"})
		wantCode(t, err, domain.CodeGitCredentialInvalidKey)
		_, err = p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "laptop", Kind: domain.GitSSHKey, Host: "github.com", PrivateKey: "not a key"})
		wantCode(t, err, domain.CodeGitCredentialInvalidKey)
		imported, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "laptop", Kind: domain.GitSSHKey, Host: "github.com",
			Username: "deployer", PrivateKey: domain.Secret(key), Passphrase: testPassphrase})
		if err != nil {
			t.Fatal(err)
		}
		if imported.Username != "deployer" || imported.PublicKey == nil || imported.Fingerprint == g.Fingerprint {
			t.Fatalf("imported %+v", imported)
		}
		list, _ := p.ListGitCredentials(ctx)
		noSecrets(t, list, key)
	})

	t.Run("names, kinds, hosts and tokens are validated", func(t *testing.T) {
		p, _ := newPort(t)
		token(t, p, "GitHub", "github.com", false)
		_, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "github", Kind: domain.GitHTTPSToken, Host: "github.com", Token: otherToken})
		wantCode(t, err, domain.CodeGitCredentialNameTaken)
		for _, in := range []ports.CreateGitCredential{
			{Name: " ", Kind: domain.GitHTTPSToken, Host: "github.com", Token: otherToken},
			{Name: "x", Kind: "password", Host: "github.com", Token: otherToken},
			{Name: "x", Kind: domain.GitHTTPSToken, Host: " ", Token: otherToken},
			{Name: "x", Kind: domain.GitHTTPSToken, Host: "github.com"},
			{Name: "x", Kind: domain.GitSSHKey, Host: "github.com"},
		} {
			_, err := p.CreateGitCredential(ctx, in)
			wantCode(t, err, "INVALID_INPUT")
		}
	})

	t.Run("at most one default per host and kind; is_default false clears it", func(t *testing.T) {
		p, _ := newPort(t)
		a := token(t, p, "a", "github.com", true)
		b := token(t, p, "b", "github.com", true)
		other := token(t, p, "c", "gitlab.com", true)
		defaults := func() map[string]bool {
			list, err := p.ListGitCredentials(ctx)
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]bool{}
			for _, c := range list.Credentials {
				m[c.ID] = c.IsDefault
			}
			return m
		}
		if d := defaults(); d[a.ID] || !d[b.ID] || !d[other.ID] {
			t.Fatalf("defaults %v: want b and c only", d)
		}
		if _, err := p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: b.ID, IsDefault: ptr(false)}); err != nil {
			t.Fatal(err)
		}
		if d := defaults(); d[a.ID] || d[b.ID] || !d[other.ID] {
			t.Fatalf("defaults %v: want c only", d)
		}
	})

	t.Run("update changes what it sets and keeps the rest", func(t *testing.T) {
		p, _ := newPort(t)
		c := token(t, p, "GitHub", "github.com", false)
		got, err := p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Name: ptr("GitHub work"), Username: ptr("octocat")})
		if err != nil {
			t.Fatal(err)
		}
		if got.Name != "GitHub work" || got.Username != "octocat" || got.Fingerprint != c.Fingerprint || got.Host != "github.com" || got.Kind != c.Kind {
			t.Fatalf("updated %+v", got)
		}
		got, err = p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Token: otherToken})
		if err != nil {
			t.Fatal(err)
		}
		if got.Fingerprint != "…0002" || got.Name != "GitHub work" {
			t.Fatalf("after a new token %+v", got)
		}
		_, err = p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, PrivateKey: "x"})
		wantCode(t, err, "INVALID_INPUT")
		_, err = p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: "missing", Name: ptr("x")})
		wantCode(t, err, domain.CodeGitCredentialNotFound)
		noSecrets(t, got)
	})

	t.Run("delete removes the credential and unpins its repositories", func(t *testing.T) {
		p, fx := newPort(t)
		c := token(t, p, "GitHub", "github.com", false)
		_, repo := fx.AddRepository(t, "", "widgets", "https://github.com/acme/widgets.git")
		if _, err := p.SetRepositoryCredential(ctx, repo, &c.ID); err != nil {
			t.Fatal(err)
		}
		wantCode(t, p.DeleteGitCredential(ctx, "missing"), domain.CodeGitCredentialNotFound)
		if err := p.DeleteGitCredential(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		list, _ := p.ListGitCredentials(ctx)
		if len(list.Credentials) != 0 {
			t.Fatalf("after delete %+v", list)
		}
		rc, err := p.GetRepositoryCredential(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Binding.CredentialID != nil || rc.Resolved != nil {
			t.Fatalf("after delete %+v", rc)
		}
	})

	t.Run("identity round trip and validation", func(t *testing.T) {
		p, _ := newPort(t)
		id, err := p.UpdateGitIdentity(ctx, " Ada Lovelace ", " ada@example.com ")
		if err != nil {
			t.Fatal(err)
		}
		if id.Name != "Ada Lovelace" || id.Email != "ada@example.com" {
			t.Fatalf("UpdateGitIdentity = %+v", id)
		}
		got, err := p.GetGitIdentity(ctx)
		if err != nil || got == nil || *got != *id {
			t.Fatalf("GetGitIdentity = %+v, %v", got, err)
		}
		for _, in := range [][2]string{{"", "a@b.c"}, {"Ada", ""}, {"Ada", "ada.example.com"}, {"Ada", "a@b@c"}} {
			_, err := p.UpdateGitIdentity(ctx, in[0], in[1])
			wantCode(t, err, "INVALID_INPUT")
		}
	})

	t.Run("a repository resolves, pins and unpins", func(t *testing.T) {
		p, fx := newPort(t)
		_, repo := fx.AddRepository(t, "", "widgets", "https://octocat:"+testToken+"@github.com/acme/widgets.git")
		rc, err := p.GetRepositoryCredential(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Binding.RepositoryID != repo || rc.Binding.CredentialID != nil || rc.Resolved != nil ||
			rc.RemoteURL == nil || *rc.RemoteURL != "https://github.com/acme/widgets.git" {
			t.Fatalf("before any credential %+v", rc)
		}
		noSecrets(t, rc)
		a := token(t, p, "a", "github.com", false)
		if rc, _ = p.GetRepositoryCredential(ctx, repo); rc.Resolved == nil || rc.Resolved.ID != a.ID {
			t.Fatalf("the only token for the host should resolve: %+v", rc)
		}
		b := token(t, p, "b", "github.com", false)
		if rc, _ = p.GetRepositoryCredential(ctx, repo); rc.Resolved != nil {
			t.Fatalf("two tokens and no default should resolve none: %+v", rc.Resolved)
		}
		rc, err = p.SetRepositoryCredential(ctx, repo, &b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Binding.CredentialID == nil || *rc.Binding.CredentialID != b.ID || rc.Resolved == nil || rc.Resolved.ID != b.ID {
			t.Fatalf("pinned %+v", rc)
		}
		gitlab := token(t, p, "gl", "gitlab.com", false)
		_, err = p.SetRepositoryCredential(ctx, repo, &gitlab.ID)
		wantCode(t, err, domain.CodeGitCredentialHostMismatch)
		_, err = p.SetRepositoryCredential(ctx, repo, ptr("missing"))
		wantCode(t, err, domain.CodeGitCredentialNotFound)
		rc, err = p.SetRepositoryCredential(ctx, repo, nil)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Binding.CredentialID != nil {
			t.Fatalf("unpinned %+v", rc)
		}
		_, err = p.GetRepositoryCredential(ctx, "missing")
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
		_, err = p.SetRepositoryCredential(ctx, "missing", nil)
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
	})

	t.Run("an ssh key serves an https origin without a port", func(t *testing.T) {
		p, fx := newPort(t)
		_, repo := fx.AddRepository(t, "", "widgets", "https://github.com/acme/widgets.git")
		key := generated(t, p, "key", "github.com")
		rc, err := p.GetRepositoryCredential(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Resolved == nil || rc.Resolved.ID != key.ID || rc.PullRequests != nil {
			t.Fatalf("only a key: resolved %+v, pull requests %+v", rc.Resolved, rc.PullRequests)
		}
		tok := token(t, p, "token", "github.com", false)
		if rc, _ = p.GetRepositoryCredential(ctx, repo); rc.Resolved == nil || rc.Resolved.ID != tok.ID || rc.PullRequests == nil || rc.PullRequests.ID != tok.ID {
			t.Fatalf("a token beats the key: resolved %+v, pull requests %+v", rc.Resolved, rc.PullRequests)
		}
		rc, err = p.SetRepositoryCredential(ctx, repo, &key.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Resolved == nil || rc.Resolved.ID != key.ID || rc.PullRequests == nil || rc.PullRequests.ID != tok.ID {
			t.Fatalf("pinned key: resolved %+v, pull requests %+v", rc.Resolved, rc.PullRequests)
		}
		noSecrets(t, rc)
	})

	t.Run("an ssh key is refused on an https origin with a port, and a token on an ssh origin", func(t *testing.T) {
		p, fx := newPort(t)
		_, porty := fx.AddRepository(t, "", "porty", "https://git.example.com:8443/acme/widgets.git")
		_, ssh := fx.AddRepository(t, "", "ssh", "git@github.com:acme/widgets.git")
		portKey := generated(t, p, "port-key", "git.example.com:8443")
		tok := token(t, p, "token", "github.com", false)
		_, err := p.SetRepositoryCredential(ctx, porty, &portKey.ID)
		wantCode(t, err, domain.CodeGitCredentialKindMismatch)
		_, err = p.SetRepositoryCredential(ctx, ssh, &tok.ID)
		wantCode(t, err, domain.CodeGitCredentialKindMismatch)
		rc, _ := p.GetRepositoryCredential(ctx, porty)
		if rc.Resolved != nil {
			t.Fatalf("an https origin with a port resolved %+v", rc.Resolved)
		}
	})

	t.Run("an ssh origin resolves an ssh key; a repository without origin can be pinned", func(t *testing.T) {
		p, fx := newPort(t)
		project, sshRepo := fx.AddRepository(t, "", "widgets", "git@github.com:acme/widgets.git")
		_, bare := fx.AddRepository(t, project, "scratch", "")
		token(t, p, "token", "github.com", false)
		key := generated(t, p, "key", "github.com")
		rc, err := p.GetRepositoryCredential(ctx, sshRepo)
		if err != nil {
			t.Fatal(err)
		}
		if rc.Resolved == nil || rc.Resolved.ID != key.ID {
			t.Fatalf("ssh origin resolved %+v", rc.Resolved)
		}
		rc, err = p.SetRepositoryCredential(ctx, bare, &key.ID)
		if err != nil {
			t.Fatal(err)
		}
		if rc.RemoteURL != nil || rc.Binding.CredentialID == nil || rc.Resolved == nil || rc.Resolved.ID != key.ID {
			t.Fatalf("pin without origin %+v", rc)
		}
	})

	t.Run("the host of a pinned credential cannot change", func(t *testing.T) {
		p, fx := newPort(t)
		c := token(t, p, "GitHub", "github.com", false)
		_, repo := fx.AddRepository(t, "", "widgets", "https://github.com/acme/widgets.git")
		if _, err := p.SetRepositoryCredential(ctx, repo, &c.ID); err != nil {
			t.Fatal(err)
		}
		_, err := p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Host: ptr("gitlab.com")})
		wantCode(t, err, domain.CodeGitCredentialHostMismatch)
		d := detailsOf(t, err)
		repos, _ := d["repositories"].([]any)
		if len(repos) != 1 {
			t.Fatalf("details %v: want the one pinned repository", d)
		}
		r, _ := repos[0].(map[string]any)
		if r["id"] != repo || r["name"] != "widgets" {
			t.Fatalf("details repository %v", r)
		}
		if _, err := p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Host: ptr("GitHub.com")}); err != nil {
			t.Fatalf("the same host, spelled otherwise: %v", err)
		}
	})

	t.Run("a project's repositories are listed in its order", func(t *testing.T) {
		p, fx := newPort(t)
		project, first := fx.AddRepository(t, "", "api", "https://github.com/acme/api.git")
		_, second := fx.AddRepository(t, project, "web", "git@github.com:acme/web.git")
		_, third := fx.AddRepository(t, project, "local", "")
		fx.AddRepository(t, "", "elsewhere", "https://github.com/acme/other.git")
		c := token(t, p, "GitHub", "github.com", false)
		list, err := p.ListRepositoryCredentials(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 3 || list[0].Binding.RepositoryID != first || list[1].Binding.RepositoryID != second || list[2].Binding.RepositoryID != third {
			t.Fatalf("ListRepositoryCredentials = %d entries: %+v", len(list), list)
		}
		if list[0].Resolved == nil || list[0].Resolved.ID != c.ID || list[1].Resolved != nil || list[2].RemoteURL != nil {
			t.Fatalf("resolutions %+v %+v %+v", list[0], list[1], list[2])
		}
		one, _ := p.GetRepositoryCredential(ctx, first)
		a, _ := json.Marshal(one)
		b, _ := json.Marshal(list[0])
		if string(a) != string(b) {
			t.Fatalf("get %s, list %s", a, b)
		}
		_, err = p.ListRepositoryCredentials(ctx, "missing")
		wantCode(t, err, "PROJECT_NOT_FOUND")
	})

	t.Run("a locked store answers reads and refuses what needs a secret", func(t *testing.T) {
		p, fx := newPort(t)
		c := token(t, p, "GitHub", "github.com", false)
		_, repo := fx.AddRepository(t, "", "widgets", "https://github.com/acme/widgets.git")
		if list, err := p.ListGitCredentials(ctx); err != nil || list.Locked {
			t.Fatalf("list before locking: %+v, %v; want not locked", list, err)
		}
		fx.Lock(t)

		if list, err := p.ListGitCredentials(ctx); err != nil || len(list.Credentials) != 1 || !list.Locked {
			t.Fatalf("list while locked: %+v, %v; want the credential, locked", list, err)
		}
		if _, err := p.UpdateGitIdentity(ctx, "Ada", "ada@example.com"); err != nil {
			t.Fatalf("identity while locked: %v", err)
		}
		if _, err := p.GetGitIdentity(ctx); err != nil {
			t.Fatalf("get identity while locked: %v", err)
		}
		if _, err := p.SetRepositoryCredential(ctx, repo, &c.ID); err != nil {
			t.Fatalf("pin while locked: %v", err)
		}
		if rc, err := p.GetRepositoryCredential(ctx, repo); err != nil || rc.Resolved == nil {
			t.Fatalf("get repository credential while locked: %+v, %v", rc, err)
		}
		if _, err := p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Name: ptr("Renamed")}); err != nil {
			t.Fatalf("an update without a secret while locked: %v", err)
		}

		_, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "new", Kind: domain.GitHTTPSToken, Host: "github.com", Token: otherToken})
		wantCode(t, err, domain.CodeGitCredentialStoreLocked)
		_, err = p.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Token: otherToken})
		wantCode(t, err, domain.CodeGitCredentialStoreLocked)
		_, err = p.TestGitCredential(ctx, c.ID, "")
		wantCode(t, err, domain.CodeGitCredentialStoreLocked)

		if err := p.DeleteGitCredential(ctx, c.ID); err != nil {
			t.Fatalf("delete while locked: %v", err)
		}
	})

	t.Run("testing an unknown credential or repository", func(t *testing.T) {
		p, _ := newPort(t)
		_, err := p.TestGitCredential(ctx, "missing", "")
		wantCode(t, err, domain.CodeGitCredentialNotFound)
		c := token(t, p, "GitHub", "github.com", false)
		_, err = p.TestGitCredential(ctx, c.ID, "missing")
		wantCode(t, err, "REPOSITORY_NOT_FOUND")
	})

	t.Run("the feed carries credentials, deletions and the identity, never a secret", func(t *testing.T) {
		p, _ := newPort(t)
		fctx, cancel := context.WithCancel(ctx)
		defer cancel()
		changes, err := p.FollowGitCredentials(fctx)
		if err != nil {
			t.Fatal(err)
		}
		next := func() ports.GitCredentialChange {
			t.Helper()
			select {
			case ch, ok := <-changes:
				if !ok {
					t.Fatal("the feed closed")
				}
				noSecrets(t, ch.Credential)
				return ch
			case <-time.After(5 * time.Second):
				t.Fatal("no change on the feed")
			}
			return ports.GitCredentialChange{}
		}
		c := token(t, p, "GitHub", "github.com", false)
		if ch := next(); ch.Credential == nil || ch.Credential.ID != c.ID || ch.Credential.Fingerprint != c.Fingerprint || ch.Deleted {
			t.Fatalf("after create %+v", ch)
		}
		if _, err := p.UpdateGitIdentity(ctx, "Ada", "ada@example.com"); err != nil {
			t.Fatal(err)
		}
		if ch := next(); ch.Identity == nil || ch.Identity.Email != "ada@example.com" || ch.Credential != nil {
			t.Fatalf("after identity %+v", ch)
		}
		if err := p.DeleteGitCredential(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		if ch := next(); !ch.Deleted || ch.Credential == nil || ch.Credential.ID != c.ID {
			t.Fatalf("after delete %+v", ch)
		}
	})
}

package gitcredentials_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/gitcli"
	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func withFiles(t *testing.T) (*gitcredentialstest.SQLite, string) {
	t.Helper()
	env := gitcredentialstest.NewSQLite(t)
	root := filepath.Join(env.Dir, "git")
	env.Service.Files = &gitcli.AuthFiles{Root: root, Inherited: []string{}}
	return env, root
}

func envOf(env []string, k string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, k+"="); ok {
			return v
		}
	}
	return ""
}

func TestResolveFindsThePushCredentialAndTheGitHubToken(t *testing.T) {
	env, _ := withFiles(t)
	_, httpsRepo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	_, sshRepo := env.AddRepository(t, "", "web", "git@github.com:acme/web.git")
	tok := githubToken(t, env.Service, "GitHub")
	key, err := env.Service.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true})
	if err != nil {
		t.Fatal(err)
	}
	res, err := env.Service.Resolve(ctx, httpsRepo, "https://github.com/acme/api.git")
	if err != nil || res.Push == nil || res.Push.ID != tok.ID || res.GitHub == nil || res.GitHub.ID != tok.ID {
		t.Fatalf("https origin: %+v, %v", res, err)
	}
	res, err = env.Service.Resolve(ctx, sshRepo, "git@github.com:acme/web.git")
	if err != nil || res.Push == nil || res.Push.ID != key.ID || res.GitHub == nil || res.GitHub.ID != tok.ID {
		t.Fatalf("ssh origin pushes with the key and calls GitHub with the token: %+v, %v", res, err)
	}
}

func TestAnOperationGetsItsOwnDirectoryAndTheTokenInMemory(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	githubToken(t, env.Service, "GitHub")
	op, err := env.Service.Operation(ctx, repo, "https://github.com/acme/api.git")
	if err != nil {
		t.Fatal(err)
	}
	if op.GitHubToken.Reveal() != "ghp_servicetest0001" || op.Push == nil {
		t.Fatalf("operation %+v", op)
	}
	dir := filepath.Dir(envOf(op.Env, "GIT_CONFIG_GLOBAL"))
	if !strings.HasPrefix(dir, filepath.Join(root, "ops")) {
		t.Fatalf("operation directory %s is not under %s/ops", dir, root)
	}
	op.Release()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("Release left the operation's directory")
	}

	env.Lock(t)
	if _, err := env.Service.Operation(ctx, repo, "https://github.com/acme/api.git"); errs.Code(err) != domain.CodeGitCredentialStoreLocked {
		t.Fatalf("an operation on a locked store: %v, want STORE_LOCKED", err)
	}
}

func TestASessionsFilesFollowEveryChangeInPlace(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	sg, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sg.Note, "no stored credential for github.com (https_token)") {
		t.Fatalf("note with nothing stored: %q", sg.Note)
	}
	if envOf(sg.Env, "GH_TOKEN") != gitcli.NoGitHubToken {
		t.Fatalf("GH_TOKEN = %q, want the placeholder", envOf(sg.Env, "GH_TOKEN"))
	}
	dir := filepath.Join(root, "sessions", "s1")
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	if read("https") != "" {
		t.Fatal("an https file with nothing stored")
	}

	// A token stored after the session started reaches its next git.
	c := githubToken(t, env.Service, "GitHub")
	if !strings.Contains(read("https"), "password=ghp_servicetest0001") {
		t.Fatalf("after storing a token, https = %q", read("https"))
	}
	if _, err := env.Service.UpdateGitIdentity(ctx, "Ada", "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read("gitconfig"), `name = "Ada"`) {
		t.Fatalf("after setting the identity, gitconfig = %q", read("gitconfig"))
	}
	if _, err := env.Service.UpdateGitCredential(ctx, ports.UpdateGitCredential{CredentialID: c.ID, Token: "ghp_rotated000002"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read("https"), "password=ghp_rotated000002") {
		t.Fatalf("after rotating, https = %q", read("https"))
	}
	again, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil || again.Note != "" || again.EnvFile != sg.EnvFile || envOf(again.Env, "GH_TOKEN") != "ghp_rotated000002" {
		t.Fatalf("resumed: %+v, %v", again, err)
	}
	if err := env.Service.DeleteGitCredential(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if read("https") != "" {
		t.Fatal("a deleted token is still in the session's files")
	}

	// The session ends: its directory goes.
	bus := eventbus.New()
	env.Service.Subscribe(bus)
	if err := bus.Publish(ctx, domain.SessionEnded{SessionID: "s1", Status: domain.SessionDone}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the directory outlived its session")
	}
}

func TestALockedStoreStartsSessionsWithoutCredentials(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	githubToken(t, env.Service, "GitHub")
	env.Lock(t)
	sg, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sg.Note, "credential store is locked") || envOf(sg.Env, "GH_TOKEN") != gitcli.NoGitHubToken {
		t.Fatalf("locked session: note %q, GH_TOKEN %q", sg.Note, envOf(sg.Env, "GH_TOKEN"))
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "s1", "https")); !os.IsNotExist(err) {
		t.Fatal("a locked store wrote an https file")
	}
}

func TestTheSweepKeepsOnlyRunningSessions(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	for _, id := range []string{"running", "ended"} {
		if _, err := env.Service.SessionEnv(ctx, id, repo); err != nil {
			t.Fatal(err)
		}
	}
	if _, release, err := env.Service.Files.WriteOperation(ports.GitAuthMaterial{}); err != nil {
		t.Fatal(err)
	} else {
		_ = release // left behind, as a crash would
	}
	err := env.Service.Sweep(ctx, func(id string) (string, bool) { return repo, id == "running" })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "sessions", "running")); err != nil {
		t.Fatal("the sweep removed a running session's directory")
	}
	for _, gone := range []string{filepath.Join(root, "sessions", "ended"), filepath.Join(root, "ops")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("the sweep left %s", gone)
		}
	}
	// The running one is kept fresh again.
	githubToken(t, env.Service, "GitHub")
	if b, _ := os.ReadFile(filepath.Join(root, "sessions", "running", "https")); !strings.Contains(string(b), "ghp_servicetest0001") {
		t.Fatal("a swept-in running session is not rewritten on change")
	}
}

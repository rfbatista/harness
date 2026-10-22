package gitcredentials_test

import (
	"context"
	"os"
	"os/exec"
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
	sockets, err := os.MkdirTemp("/tmp", "hg") // socket paths are short on macOS
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockets) })
	broker, err := gitcli.NewAuthBroker(root, "")
	if err != nil {
		t.Fatal(err)
	}
	broker.SocketRoot, broker.Inherited = sockets, []string{}
	env.Service.Broker = broker
	return env, root
}

// password is what the session's git is answered for github.com now; ""
// when nothing.
func password(t *testing.T, env []string) string {
	t.Helper()
	cmd := exec.Command("git", "credential", "fill")
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return envOf(strings.Split(string(out), "\n"), "password")
}

// noSecretUnder fails t when a file under dir holds secret.
func noSecretUnder(t *testing.T, dir, secret string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
		if err == nil && e.Type().IsRegular() {
			if b, _ := os.ReadFile(path); strings.Contains(string(b), secret) {
				t.Errorf("%s holds a secret", path)
			}
		}
		return nil
	})
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

func TestASessionIsServedEveryChangeWithNoSecretOnDisk(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	sg, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sg.Note, "no stored token or SSH key for github.com") {
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
	if got := password(t, sg.Env); got != "" {
		t.Fatalf("with nothing stored, git got %q", got)
	}

	// A token stored after the session started reaches its next git.
	c := githubToken(t, env.Service, "GitHub")
	if got := password(t, sg.Env); got != "ghp_servicetest0001" {
		t.Fatalf("after storing a token, git got %q", got)
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
	if got := password(t, sg.Env); got != "ghp_rotated000002" {
		t.Fatalf("after rotating, git got %q", got)
	}
	noSecretUnder(t, root, "ghp_")
	noSecretUnder(t, env.Service.Broker.(*gitcli.AuthBroker).SocketRoot, "ghp_")
	again, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil || again.Note != "" || again.EnvFile != sg.EnvFile || envOf(again.Env, "GH_TOKEN") != "ghp_rotated000002" {
		t.Fatalf("resumed: %+v, %v", again, err)
	}
	if err := env.Service.DeleteGitCredential(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := password(t, sg.Env); got != "" {
		t.Fatalf("a deleted token is still served: %q", got)
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
	if got := password(t, sg.Env); got != "" {
		t.Fatalf("a locked store served %q", got)
	}
	_ = root
}

func TestTheSweepKeepsOnlyRunningSessions(t *testing.T) {
	env, root := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	for _, id := range []string{"running", "ended"} {
		if _, err := env.Service.SessionEnv(ctx, id, repo); err != nil {
			t.Fatal(err)
		}
	}
	if _, release, err := env.Service.Broker.OpenOperation(func() (ports.GitAuthMaterial, error) { return ports.GitAuthMaterial{}, nil }); err != nil {
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
	// The running one is served again, what is stored now.
	githubToken(t, env.Service, "GitHub")
	sg, err := env.Service.SessionEnv(ctx, "running", repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := password(t, sg.Env); got != "ghp_servicetest0001" {
		t.Fatalf("a swept-in running session is served %q", got)
	}
}

// originURL is what git answers for origin in a checkout whose remote is
// remote, run with env: the URL after the harness's own rewrites.
func originURL(t *testing.T, env []string, remote string) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) []byte {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	run("init", "-q")
	run("remote", "add", "origin", remote)
	return strings.TrimSpace(string(run("ls-remote", "--get-url", "origin")))
}

func sshKey(t *testing.T, p ports.GitCredentials, name, host, username string) *domain.GitCredential {
	t.Helper()
	c, err := p.CreateGitCredential(ctx, ports.CreateGitCredential{Name: name, Kind: domain.GitSSHKey, Host: host, Username: username, Generate: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnSSHKeyServesAnHTTPSOriginInASessionUntilATokenIsAdded(t *testing.T) {
	const https = "https://github.com/acme/api.git"
	env, _ := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", https)
	_, other := env.AddRepository(t, "", "gitlab", "https://gitlab.com/acme/api.git")
	sshKey(t, env.Service, "key", "github.com", "octocat")

	sg, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if sg.Note != "" {
		t.Fatalf("a key serves the origin, so there is nothing to tell the agent: %q", sg.Note)
	}
	if got := originURL(t, sg.Env, https); got != "octocat@github.com:acme/api.git" {
		t.Fatalf("with only a key, git reaches %q", got)
	}
	if got := originURL(t, sg.Env, "https://gitlab.com/acme/api.git"); got != "https://gitlab.com/acme/api.git" {
		t.Fatalf("another host is rewritten too: %q", got)
	}

	// The running session follows: a token for the host takes the origin back.
	tok := githubToken(t, env.Service, "GitHub")
	if got := originURL(t, sg.Env, https); got != https {
		t.Fatalf("with a token, git reaches %q", got)
	}
	// And so does pinning the key.
	if _, err := env.Service.SetRepositoryCredential(ctx, repo, ptr(sshKeyID(t, env.Service))); err != nil {
		t.Fatal(err)
	}
	if got := originURL(t, sg.Env, https); got != "octocat@github.com:acme/api.git" {
		t.Fatalf("pinned key: git reaches %q", got)
	}
	if _, err := env.Service.SetRepositoryCredential(ctx, repo, nil); err != nil {
		t.Fatal(err)
	}
	if got := originURL(t, sg.Env, https); got != https {
		t.Fatalf("unpinned: git reaches %q", got)
	}
	if err := env.Service.DeleteGitCredential(ctx, tok.ID); err != nil {
		t.Fatal(err)
	}
	if got := originURL(t, sg.Env, https); got != "octocat@github.com:acme/api.git" {
		t.Fatalf("token deleted: git reaches %q", got)
	}
	// The repository of another host is untouched by all of it.
	og, err := env.Service.SessionEnv(ctx, "s2", other)
	if err != nil {
		t.Fatal(err)
	}
	if got := originURL(t, og.Env, "https://gitlab.com/acme/api.git"); got != "https://gitlab.com/acme/api.git" {
		t.Fatalf("a repository of another host: %q", got)
	}
}

func sshKeyID(t *testing.T, p ports.GitCredentials) string {
	t.Helper()
	list, err := p.ListGitCredentials(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list.Credentials {
		if c.Kind == domain.GitSSHKey {
			return c.ID
		}
	}
	t.Fatal("no ssh key")
	return ""
}

func ptr[T any](v T) *T { return &v }

func TestAnSSHKeyServesAnHTTPSOriginInAnOperation(t *testing.T) {
	const https = "https://github.com/acme/api.git"
	env, _ := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", https)
	sshKey(t, env.Service, "key", "github.com", "")
	op, err := env.Service.Operation(ctx, repo, https)
	if err != nil {
		t.Fatal(err)
	}
	defer op.Release()
	if got := originURL(t, op.Env, https); got != "git@github.com:acme/api.git" {
		t.Fatalf("an operation with only a key reaches %q", got)
	}
	githubToken(t, env.Service, "GitHub")
	op2, err := env.Service.Operation(ctx, repo, https)
	if err != nil {
		t.Fatal(err)
	}
	defer op2.Release()
	if got := originURL(t, op2.Env, https); got != https {
		t.Fatalf("an operation with a token reaches %q", got)
	}
}

func TestAnHTTPSOriginWithAPortIsNeverRewritten(t *testing.T) {
	const https = "https://git.example.com:8443/acme/api.git"
	env, _ := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", https)
	sshKey(t, env.Service, "key", "git.example.com", "")
	sg, err := env.Service.SessionEnv(ctx, "s1", repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := originURL(t, sg.Env, https); got != https {
		t.Fatalf("an origin with a port reaches %q", got)
	}
	if !strings.Contains(sg.Note, "no stored token for git.example.com:8443") {
		t.Fatalf("nothing resolves, the note should say what to add: %q", sg.Note)
	}
}

// recordingTester answers ok and remembers what it was asked.
type recordingTester struct{ got []string }

func (r *recordingTester) Test(_ context.Context, c ports.GitCredentialSecret, remoteURL string) (*ports.GitCredentialTest, error) {
	r.got = append(r.got, string(c.Kind)+" "+remoteURL)
	return &ports.GitCredentialTest{OK: true, Message: "ok", CheckedURL: remoteURL}, nil
}

func TestTestingAnSSHKeyAgainstAnHTTPSRepositoryChecksSSHToTheHost(t *testing.T) {
	env, _ := withFiles(t)
	_, repo := env.AddRepository(t, "", "api", "https://github.com/acme/api.git")
	key := sshKey(t, env.Service, "key", "github.com", "octocat")
	tok := githubToken(t, env.Service, "GitHub")
	tester := &recordingTester{}
	env.Service.Tester = tester
	for _, id := range []string{key.ID, tok.ID} {
		if _, err := env.Service.TestGitCredential(ctx, id, repo); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"ssh_key octocat@github.com:acme/api.git", "https_token https://github.com/acme/api.git"}
	if len(tester.got) != 2 || tester.got[0] != want[0] || tester.got[1] != want[1] {
		t.Fatalf("tester asked %q, want %q", tester.got, want)
	}
}

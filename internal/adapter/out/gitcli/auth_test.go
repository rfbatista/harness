package gitcli

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// machine is a planted machine setup: everything the harness must never use.
// Each credential source touches a marker file when something reads it, and
// the ssh-agent socket records connections.
type machine struct {
	home, markers string
	env           []string // the server process's environment on this machine
	agentConns    *int
	mu            *sync.Mutex
}

func plantMachine(t *testing.T) *machine {
	t.Helper()
	home := t.TempDir()
	markers := t.TempDir()
	touch := func(name string) string { return "touch " + filepath.Join(markers, name) }
	helper := func(name string) string {
		return `!f() { ` + touch(name) + `; echo username=machine; echo password=machine-secret; }; f`
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".gitconfig"), "[credential]\n\thelper = \""+helper("global-helper")+"\"\n"+
		"[core]\n\tsshCommand = \""+touch("global-sshcommand")+"; false\"\n"+
		"[user]\n\tname = Machine User\n\temail = machine@example.com\n"+
		"[include]\n\tpath = "+filepath.Join(home, "included.gitconfig")+"\n")
	write(filepath.Join(home, "included.gitconfig"), "[credential]\n\thelper = \""+helper("included-helper")+"\"\n")
	write(filepath.Join(home, "xdg", "git", "config"), "[credential]\n\thelper = \""+helper("xdg-helper")+"\"\n")
	system := filepath.Join(home, "system.gitconfig") // stands in for /etc/gitconfig and Apple git's osxkeychain
	write(system, "[credential]\n\thelper = \""+helper("system-helper")+"\"\n")
	write(filepath.Join(home, ".ssh", "config"), "Host *\n  ProxyCommand sh -c '"+touch("ssh-config")+"; exit 1'\n")
	write(filepath.Join(home, ".ssh", "id_ed25519"), "not really a key\n")

	sock := filepath.Join(t.TempDir(), "agent.sock")
	if len(sock) > 100 { // unix socket paths are short on macOS
		dir, err := os.MkdirTemp("/tmp", "agent")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		sock = filepath.Join(dir, "a.sock")
	}
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	m := &machine{home: home, markers: markers, agentConns: new(int), mu: &sync.Mutex{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			m.mu.Lock()
			*m.agentConns++
			m.mu.Unlock()
			c.Close()
		}
	}()
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		k, _, _ := strings.Cut(kv, "=")
		return strings.HasPrefix(k, "GIT_") || k == "HOME" || k == "XDG_CONFIG_HOME" || k == "SSH_AUTH_SOCK"
	})
	m.env = append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, "xdg"), "GIT_CONFIG_SYSTEM="+system,
		"SSH_AUTH_SOCK="+sock, "GITHUB_TOKEN=machine-gh-token", "GIT_AUTHOR_NAME=Machine Author")
	return m
}

// reset forgets what was used so far.
func (m *machine) reset(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(m.markers)
	for _, e := range entries {
		if err := os.Remove(filepath.Join(m.markers, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	m.mu.Lock()
	*m.agentConns = 0
	m.mu.Unlock()
}

// untouched fails t when any machine credential source was read.
func (m *machine) untouched(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(m.markers)
	for _, e := range entries {
		t.Errorf("the machine's %s was used", e.Name())
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if *m.agentConns != 0 {
		t.Errorf("the machine's ssh-agent was asked %d times", *m.agentConns)
	}
}

// gitServer is a smart-HTTP git server (git http-backend) that requires
// Basic auth with one login, and records every Authorization it was sent.
type gitServer struct {
	URL  string
	mu   sync.Mutex
	auth []string
}

func (s *gitServer) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = nil
}

func (s *gitServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.auth)
}

func newGitServer(t *testing.T, user, password string) *gitServer {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "widgets.git")
	mustGit(t, "", nil, "init", "-q", "--bare", bare)
	mustGit(t, bare, nil, "config", "http.receivepack", "true")
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{Path: gitBin, Args: []string{"http-backend"},
		Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "HOME=" + t.TempDir()}}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
	s := &gitServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		s.mu.Lock()
		s.auth = append(s.auth, got)
		s.mu.Unlock()
		if got != want {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Header.Set("Authorization", "") // http-backend does not need it
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	s.URL = srv.URL + "/widgets.git"
	return s
}

// mustGit runs git with env (nil: this process's) and fails t on error.
func mustGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, env, args...)
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func runGit(dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = strings.NewReader("") // a prompt would read this and fail, never hang
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// checkout is a commit to push, with a repo-local credential helper planted
// in its .git/config as the machine's own setup might have it.
func checkout(t *testing.T, m *machine, origin string) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, nil, "init", "-q", "-b", "main")
	mustGit(t, dir, nil, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "first")
	mustGit(t, dir, nil, "remote", "add", "origin", origin)
	mustGit(t, dir, nil, "config", "credential.helper",
		`!f() { touch `+filepath.Join(m.markers, "repo-local-helper")+`; echo username=machine; echo password=machine-secret; }; f`)
	return dir
}

func authFiles(t *testing.T, m *machine) *AuthFiles {
	return &AuthFiles{Root: filepath.Join(t.TempDir(), "git"), Inherited: m.env}
}

const storedToken = "ghp_storedHarnessToken42"

// The planted machine has a credential helper in every config file git
// reads, an ssh config, a key, an agent and a token in the environment. A
// push with the harness's env authenticates with the stored token and
// nothing else, and with no stored token it fails without a prompt.
func TestAPushUsesOnlyTheStoredCredential(t *testing.T) {
	m := plantMachine(t)
	srv := newGitServer(t, "x-access-token", storedToken)
	host := strings.TrimPrefix(strings.TrimPrefix(srv.URL, "http://"), "https://")
	host = host[:strings.IndexByte(host, '/')]
	files := authFiles(t, m)
	dir := checkout(t, m, srv.URL)

	// Control: without the harness's environment, git does use the planted
	// machine — so the checks below can fail.
	if out, err := runGit(dir, m.env, "push", "origin", "main:refs/heads/control"); err == nil {
		t.Fatalf("the planted machine's credential was accepted: %s", out)
	}
	if entries, _ := os.ReadDir(m.markers); len(entries) == 0 {
		t.Fatal("the planted machine was not used without the harness's environment; the test proves nothing")
	}
	m.reset(t)
	srv.forget()

	for _, kind := range []string{"operation", "session"} {
		t.Run(kind, func(t *testing.T) {
			write := func(mat ports.GitAuthMaterial) []string {
				t.Helper()
				if kind == "session" {
					env, _, err := files.WriteSession("s-push", mat)
					if err != nil {
						t.Fatal(err)
					}
					return env
				}
				env, release, err := files.WriteOperation(mat)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(release)
				return env
			}
			// No stored credential: the push fails, as the host decides, and
			// nothing of the machine's was tried.
			env := append(slices.Clone(m.env), write(ports.GitAuthMaterial{})...)
			if out, err := runGit(dir, env, "push", "origin", "main:refs/heads/none-"+kind); err == nil {
				t.Fatalf("a push with no stored credential succeeded: %s", out)
			}
			for _, a := range srv.seen() {
				if strings.Contains(decodeBasic(a), "machine") {
					t.Fatalf("the server was sent the machine's credential: %q", decodeBasic(a))
				}
			}
			m.untouched(t)

			// The stored token: the push lands, sent with that token only.
			env = append(slices.Clone(m.env), write(ports.GitAuthMaterial{
				HTTPS: &ports.GitHTTPSLogin{Host: host, Username: "x-access-token", Token: storedToken},
			})...)
			mustGit(t, dir, env, "push", "origin", "main:refs/heads/pushed-"+kind)
			for _, a := range srv.seen() {
				if a != "" && decodeBasic(a) != "x-access-token:"+storedToken {
					t.Fatalf("the server was sent %q", decodeBasic(a))
				}
			}
			m.untouched(t)
		})
	}
}

// A token stored for another host is never offered.
func TestTheHelperAnswersOnlyForItsHost(t *testing.T) {
	m := plantMachine(t)
	srv := newGitServer(t, "x-access-token", storedToken)
	files := authFiles(t, m)
	dir := checkout(t, m, srv.URL)
	env, release, err := files.WriteOperation(ports.GitAuthMaterial{
		HTTPS: &ports.GitHTTPSLogin{Host: "github.com", Username: "x-access-token", Token: storedToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if out, err := runGit(dir, append(slices.Clone(m.env), env...), "ls-remote", "origin"); err == nil {
		t.Fatalf("ls-remote with a token for another host succeeded: %s", out)
	}
	for _, a := range srv.seen() {
		if a != "" {
			t.Fatalf("the server was sent %q", decodeBasic(a))
		}
	}
	m.untouched(t)
}

// Over ssh, neither the machine's ssh config, its keys, its agent nor its
// core.sshCommand is used: ssh runs with the harness's options only.
func TestAnSSHPushUsesNothingOfTheMachine(t *testing.T) {
	m := plantMachine(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { // a host that hangs up: there is no ssh server to reach
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	dir := checkout(t, m, "ssh://git@127.0.0.1:"+strconv.Itoa(port)+"/acme/widgets.git")
	// Control: on its own, git runs the machine's core.sshCommand.
	_, _ = runGit(dir, m.env, "push", "origin", "main")
	if _, err := os.Stat(filepath.Join(m.markers, "global-sshcommand")); err != nil {
		t.Fatal("the planted core.sshCommand was not used without the harness's environment; the test proves nothing")
	}
	m.reset(t)

	files := authFiles(t, m)
	env, release, err := files.WriteOperation(ports.GitAuthMaterial{})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if out, err := runGit(dir, append(slices.Clone(m.env), env...), "push", "origin", "main"); err == nil {
		t.Fatalf("an ssh push to nowhere succeeded: %s", out)
	}
	m.untouched(t)
}

// git's own reads of a checkout (history, code views, worktrees) never
// authenticate: the machine's helpers and ssh setup are switched off for
// them too, though the rest of its config still applies.
func TestLocalCommandsCannotAuthenticate(t *testing.T) {
	m := plantMachine(t)
	srv := newGitServer(t, "machine", "machine-secret")
	dir := checkout(t, m, srv.URL)
	for _, kv := range m.env {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	// Control: a plain git command uses the machine's helper.
	_, _ = runGit(dir, nil, "ls-remote", "origin")
	if entries, _ := os.ReadDir(m.markers); len(entries) == 0 {
		t.Fatal("the planted helpers were not used by a plain git; the test proves nothing")
	}
	m.reset(t)
	if out, err := gitCmd(context.Background(), dir, "ls-remote", "origin").CombinedOutput(); err == nil {
		t.Fatalf("a local command authenticated with the machine's helper: %s", out)
	}
	m.untouched(t)
	// The machine's config is still read for everything else.
	out, err := gitCmd(context.Background(), dir, "config", "user.email").Output()
	if err != nil || strings.TrimSpace(string(out)) != "machine@example.com" {
		t.Fatalf("user.email = %q, %v: local commands should still read the machine's config", out, err)
	}
}

func decodeBasic(h string) string {
	b, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Basic "))
	return string(b)
}

// Files are owner-only, outside the worktree, rewritten in place, and the
// environment points at them.
func TestSessionDirectoriesAreOwnerOnlyAndStable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "git")
	files := &AuthFiles{Root: root, ExcludesFile: "/Users/ada/.gitignore_global", Inherited: []string{"GIT_COMMITTER_EMAIL=old@example.com"}}
	id := &domain.GitIdentity{Name: `Ada "Countess" Lovelace`, Email: "ada@example.com"}
	env, envPath, err := files.WriteSession("s1", ports.GitAuthMaterial{
		Identity: id, HTTPS: &ports.GitHTTPSLogin{Host: "github.com", Username: "x-access-token", Token: storedToken},
		SSHKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----", GitHubToken: storedToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "sessions", "s1")
	for _, name := range []string{gitconfigFile, httpsFile, sshKeyFile, envFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v, %v; want mode 600", name, info, err)
		}
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0o700 {
		t.Fatalf("session dir mode %o, want 700", info.Mode().Perm())
	}
	if envPath != filepath.Join(dir, "env.sh") {
		t.Fatalf("env file %s", envPath)
	}
	want := map[string]string{
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": filepath.Join(dir, "gitconfig"), "GIT_CONFIG_COUNT": "2",
		"GIT_CONFIG_KEY_0": "credential.helper", "GIT_CONFIG_VALUE_0": "", "GIT_CONFIG_KEY_1": "credential.helper",
		"GIT_TERMINAL_PROMPT": "0", "SSH_AUTH_SOCK": "", "GIT_ASKPASS": "", "EMAIL": "",
		"GH_TOKEN": storedToken, "GITHUB_TOKEN": storedToken, "GH_CONFIG_DIR": filepath.Join(dir, "gh"),
		"GIT_COMMITTER_EMAIL": "ada@example.com", "HARNESS_GIT_ENV": envPath,
	}
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		got[k] = v
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || g != v {
			t.Errorf("%s = %q (set %v), want %q", k, g, ok, v)
		}
	}
	if _, ok := got["GIT_AUTHOR_NAME"]; ok {
		t.Error("GIT_AUTHOR_NAME is set though the server's environment has none")
	}
	if !strings.Contains(got["GIT_SSH_COMMAND"], "IdentityFile="+filepath.Join(dir, "ssh_key")) ||
		!strings.Contains(got["GIT_SSH_COMMAND"], "IdentityAgent=none") || !strings.Contains(got["GIT_SSH_COMMAND"], "-F /dev/null") {
		t.Errorf("GIT_SSH_COMMAND = %q", got["GIT_SSH_COMMAND"])
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, "gitconfig"))
	for _, s := range []string{`name = "Ada \"Countess\" Lovelace"`, "useConfigOnly = true", `excludesFile = "/Users/ada/.gitignore_global"`} {
		if !strings.Contains(string(cfg), s) {
			t.Errorf("gitconfig lacks %q:\n%s", s, cfg)
		}
	}
	// git reads the identity and the excludes file from it.
	gitEnv := append(os.Environ(), env...)
	if out := mustGit(t, t.TempDir(), gitEnv, "config", "user.name"); strings.TrimSpace(out) != `Ada "Countess" Lovelace` {
		t.Errorf("git reads user.name %q", out)
	}

	// Rewritten in place: same paths, new contents; a removed key is gone.
	env2, envPath2, err := files.WriteSession("s1", ports.GitAuthMaterial{})
	if err != nil {
		t.Fatal(err)
	}
	if envPath2 != envPath || strings.Join(slices.DeleteFunc(slices.Clone(env2), isToken), "\n") != strings.Join(slices.DeleteFunc(slices.Clone(env), isToken), "\n") {
		t.Fatalf("the environment changed:\n%v\n%v", env, env2)
	}
	for _, name := range []string{httpsFile, sshKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived a rewrite without it: %v", name, err)
		}
	}
	if got := envValue(env2, "GH_TOKEN"); got != NoGitHubToken {
		t.Fatalf("GH_TOKEN with no token = %q, want the placeholder", got)
	}
	if ids, _ := files.SessionIDs(); len(ids) != 1 || ids[0] != "s1" {
		t.Fatalf("SessionIDs = %v", ids)
	}
	if err := files.RemoveSession("s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the session directory outlived RemoveSession")
	}
	if _, _, err := files.WriteSession("../escape", ports.GitAuthMaterial{}); err == nil {
		t.Fatal("a session id with a path in it was accepted")
	}
}

func isToken(kv string) bool {
	k, _, _ := strings.Cut(kv, "=")
	return slices.Contains(ghTokenVars, k) || slices.Contains(identityVars, k)
}

func envValue(env []string, k string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, k+"="); ok {
			return v
		}
	}
	return ""
}

// A login shell's profile may export the machine's agent or token; sourcing
// env.sh after it puts the harness's values back.
func TestTheEnvFileRestoresTheHarnessEnvironmentInAShell(t *testing.T) {
	files := &AuthFiles{Root: filepath.Join(t.TempDir(), "git"), Inherited: []string{}}
	_, envFile, err := files.WriteSession("s2", ports.GitAuthMaterial{GitHubToken: storedToken})
	if err != nil {
		t.Fatal(err)
	}
	script := `export SSH_AUTH_SOCK=/machine/agent GITHUB_TOKEN=machine GIT_SSH_COMMAND=machine-ssh; . "$1"; ` +
		`printf '%s|%s|%s|%s' "${SSH_AUTH_SOCK-unset}" "$GITHUB_TOKEN" "$GIT_CONFIG_VALUE_0" "$GIT_SSH_COMMAND"`
	out, err := exec.Command("/bin/sh", "-c", script, "sh", envFile).Output()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(string(out), "|", 4)
	if parts[0] != "unset" || parts[1] != storedToken || parts[2] != "" || !strings.Contains(parts[3], "IdentityAgent=none") {
		t.Fatalf("after sourcing env.sh: %q", out)
	}
}

// The publisher pushes with the operation's environment only, and says when
// origin refused the credential.
func TestThePublisherAuthenticatesOnlyThroughItsEnvironment(t *testing.T) {
	m := plantMachine(t)
	for k, v := range envMap(m.env) {
		t.Setenv(k, v)
	}
	srv := newGitServer(t, "x-access-token", storedToken)
	host := strings.TrimPrefix(srv.URL, "http://")
	host = host[:strings.IndexByte(host, '/')]
	dir := checkout(t, m, srv.URL)
	mustGit(t, dir, nil, "branch", "agent/x")
	p := NewBranchPublisher()
	ctx := context.Background()

	err := p.Push(ctx, dir, "agent/x", nil)
	if !errors.Is(err, ports.ErrRemoteAuthRejected) {
		t.Fatalf("push with no credentials: %v, want ErrRemoteAuthRejected", err)
	}
	files := authFiles(t, m)
	wrong, release, err := files.WriteOperation(ports.GitAuthMaterial{HTTPS: &ports.GitHTTPSLogin{Host: host, Username: "x-access-token", Token: "ghp_wrong"}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := p.Push(ctx, dir, "agent/x", wrong); !errors.Is(err, ports.ErrRemoteAuthRejected) {
		t.Fatalf("push with a refused token: %v, want ErrRemoteAuthRejected", err)
	}
	right, release2, err := files.WriteOperation(ports.GitAuthMaterial{HTTPS: &ports.GitHTTPSLogin{Host: host, Username: "x-access-token", Token: storedToken}})
	if err != nil {
		t.Fatal(err)
	}
	defer release2()
	if err := p.Push(ctx, dir, "agent/x", right); err != nil {
		t.Fatalf("push with the stored token: %v", err)
	}
	if err := p.FetchBranch(ctx, dir, "agent/x", right); err != nil {
		t.Fatalf("fetch with the stored token: %v", err)
	}
	m.untouched(t)
}

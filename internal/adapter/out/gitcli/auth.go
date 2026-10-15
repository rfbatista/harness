package gitcli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitAuthBroker = (*AuthBroker)(nil)

// NoGitHubToken is what gh's token variables hold when no token resolves:
// unset, gh would read the OS keyring, which is the machine's.
const NoGitHubToken = "harness-no-credential"

// The files of a git directory (see "Contract: Harness server ↔ Agent
// sessions — git credentials" §1). None holds a secret: the key and the
// tokens are served over the directory's sockets, read from the store each
// time git asks.
const (
	gitconfigFile = "gitconfig"
	envFile       = "env.sh"
	ghDir         = "gh"
)

// legacyFiles are what earlier versions wrote into a git directory, secrets
// among them; opening a directory removes them.
var legacyFiles = []string{"https", "ssh_key", "credential-helper"}

// The sockets of a git directory, in its socket dir.
const (
	agentSocket = "agent.sock"
	credSocket  = "cred.sock"
)

// ghTokenEnv carries the session's GitHub token in its environment, so
// env.sh can put gh's variables back after a profile without the token
// being written to it.
const ghTokenEnv = "HARNESS_GH_TOKEN"

// identityVars are the identity variables git reads before any config. The
// server's own environment may carry them; sessions get them overwritten.
var identityVars = []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"}

// blanked are machine variables that could hand git or ssh a credential or a
// prompt; every harness git process runs with them empty.
var blanked = []string{"GIT_ASKPASS", "SSH_ASKPASS", "GIT_SSH", "SSH_AUTH_SOCK", "GIT_CONFIG_PARAMETERS", "EMAIL"}

// ghTokenVars are every variable gh reads a token from.
var ghTokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// AuthBroker keeps git directories under Root (<data dir>/git): one per
// session under sessions/, one per operation under ops/, and a shared
// known_hosts. Each directory is served two unix sockets under SocketRoot:
// an ssh-agent holding the stored key and a git credential-cache answering
// the stored login, both read from the directory's source on every request.
type AuthBroker struct {
	// Root is absolute: sessions run git from their worktrees.
	Root string
	// SocketRoot is where the sockets go; "" is /tmp/harness-<uid>, short
	// enough for a socket path on every OS.
	SocketRoot string
	// ExcludesFile is the machine's global core.excludesFile path, carried
	// into every gitconfig; "" when it has none. It is a path, not a
	// credential.
	ExcludesFile string
	// Inherited is the server's own environment, which decides which
	// identity variables must be overwritten; nil reads os.Environ.
	Inherited []string

	mu     sync.Mutex
	served map[string]*servedDir // by directory
}

// NewAuthBroker returns the adapter keeping directories under root, which
// must be absolute.
func NewAuthBroker(root, excludesFile string) (*AuthBroker, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("git directories: root %q is not absolute", root)
	}
	return &AuthBroker{Root: root, ExcludesFile: excludesFile}, nil
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (a *AuthBroker) sessionDir(sessionID string) (string, error) {
	if !sessionIDPattern.MatchString(sessionID) || strings.Contains(sessionID, "..") {
		return "", fmt.Errorf("git directory: bad session id %q", sessionID)
	}
	return filepath.Join(a.Root, "sessions", sessionID), nil
}

func (a *AuthBroker) knownHosts() string { return filepath.Join(a.Root, "known_hosts") }

func (a *AuthBroker) socketRoot() string {
	if a.SocketRoot != "" {
		return a.SocketRoot
	}
	return filepath.Join("/tmp", "harness-"+strconv.Itoa(os.Getuid()))
}

// socketDir is dir's socket directory: named from Root and dir, so a
// session keeps its sockets across server restarts, and short.
func (a *AuthBroker) socketDir(dir string) string {
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(a.socketRoot(), a.rootTag()+"-"+hex.EncodeToString(sum[:8]))
}

// rootTag names Root's socket dirs, so a sweep removes only its own.
func (a *AuthBroker) rootTag() string {
	sum := sha256.Sum256([]byte(a.Root))
	return hex.EncodeToString(sum[:4])
}

// OpenSession writes the session's directory and serves its sockets, or, if
// it is served already, rewrites its files and swaps its source in place:
// its paths, so its env, never change for the session.
func (a *AuthBroker) OpenSession(sessionID string, src ports.GitAuthSource) ([]string, string, error) {
	dir, err := a.sessionDir(sessionID)
	if err != nil {
		return nil, "", err
	}
	env, err := a.open(dir, src)
	if err != nil {
		return nil, "", err
	}
	return env, filepath.Join(dir, envFile), nil
}

func (a *AuthBroker) CloseSession(sessionID string) error {
	dir, err := a.sessionDir(sessionID)
	if err != nil {
		return err
	}
	return a.close(dir)
}

func (a *AuthBroker) SessionIDs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(a.Root, "sessions"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (a *AuthBroker) OpenOperation(src ports.GitAuthSource) ([]string, func(), error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(a.Root, "ops", hex.EncodeToString(b[:]))
	env, err := a.open(dir, src)
	if err != nil {
		_ = a.close(dir)
		return nil, nil, err
	}
	return env, func() { _ = a.close(dir) }, nil
}

// SweepOperations removes what a previous run left: operation directories,
// and socket dirs of Root nothing serves.
func (a *AuthBroker) SweepOperations() error {
	if err := os.RemoveAll(filepath.Join(a.Root, "ops")); err != nil {
		return err
	}
	entries, err := os.ReadDir(a.socketRoot())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	inUse := map[string]bool{}
	for _, s := range a.served {
		inUse[filepath.Base(s.sockets)] = true
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), a.rootTag()+"-") && !inUse[e.Name()] {
			if err := os.RemoveAll(filepath.Join(a.socketRoot(), e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// open writes dir's files from src's material now and serves its sockets.
func (a *AuthBroker) open(dir string, src ports.GitAuthSource) ([]string, error) {
	m, err := src()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.served == nil {
		a.served = map[string]*servedDir{}
	}
	s := a.served[dir]
	if s == nil {
		if s, err = serve(a.socketDir(dir), src); err != nil {
			return nil, err
		}
		a.served[dir] = s
	}
	s.setSource(src)
	env, err := a.write(dir, s.sockets, m)
	if err != nil {
		return nil, err
	}
	return env, nil
}

// close stops serving dir and removes it and its sockets.
func (a *AuthBroker) close(dir string) error {
	a.mu.Lock()
	s := a.served[dir]
	delete(a.served, dir)
	a.mu.Unlock()
	sockets := a.socketDir(dir)
	if s != nil {
		s.close()
	}
	return errors.Join(os.RemoveAll(sockets), os.RemoveAll(dir))
}

// write fills dir and answers the environment that points git at it and at
// its sockets.
func (a *AuthBroker) write(dir, sockets string, m ports.GitAuthMaterial) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(dir, ghDir), 0o700); err != nil {
		return nil, err
	}
	for _, d := range []string{a.Root, filepath.Dir(dir), dir, filepath.Join(dir, ghDir)} {
		if err := os.Chmod(d, 0o700); err != nil {
			return nil, err
		}
	}
	for _, name := range legacyFiles {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	if err := writeFile(filepath.Join(dir, gitconfigFile), a.gitconfig(m.Identity), 0o600); err != nil {
		return nil, err
	}
	env := a.env(dir, sockets, m)
	if err := writeFile(filepath.Join(dir, envFile), envScript(env), 0o600); err != nil {
		return nil, err
	}
	return env, nil
}

func (a *AuthBroker) gitconfig(id *domain.GitIdentity) string {
	var b strings.Builder
	b.WriteString("# Written by the harness; the machine's git config is not read.\n[user]\n\tuseConfigOnly = true\n")
	if id != nil {
		fmt.Fprintf(&b, "\tname = %s\n\temail = %s\n", configValue(id.Name), configValue(id.Email))
	}
	if a.ExcludesFile != "" {
		fmt.Fprintf(&b, "[core]\n\texcludesFile = %s\n", configValue(a.ExcludesFile))
	}
	return b.String()
}

// configValue quotes v for a git config file.
func configValue(v string) string {
	v = strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\n", " ").Replace(v)
	return `"` + v + `"`
}

// env is what every git process using dir gets, on top of its own: the
// contract's §2, in order.
func (a *AuthBroker) env(dir, sockets string, m ports.GitAuthMaterial) []string {
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(dir, gitconfigFile),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1=cache --socket " + shellQuote(filepath.Join(sockets, credSocket)),
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, k := range blanked {
		env = append(env, k+"=")
	}
	env = append(env, "GIT_SSH_COMMAND="+sshCommand(filepath.Join(sockets, agentSocket), a.knownHosts()),
		"GH_CONFIG_DIR="+filepath.Join(dir, ghDir))
	token := m.GitHubToken.Reveal()
	if token == "" {
		token = NoGitHubToken
	}
	env = append(env, ghTokenEnv+"="+token)
	for _, k := range ghTokenVars {
		env = append(env, k+"="+token)
	}
	inherited := a.Inherited
	if inherited == nil {
		inherited = os.Environ()
	}
	for _, k := range identityVars {
		if !hasVar(inherited, k) {
			continue
		}
		v := ""
		if m.Identity != nil {
			v = m.Identity.Name
			if strings.HasSuffix(k, "_EMAIL") {
				v = m.Identity.Email
			}
		}
		env = append(env, k+"="+v)
	}
	return append(env, "HARNESS_GIT_ENV="+filepath.Join(dir, envFile))
}

// sshCommand is ssh with nothing of the machine's: no config, no default
// key (IdentityFile=/dev/null keeps ssh from adding ~/.ssh/id_*), only the
// harness's agent, never a prompt, and the harness's known_hosts.
func sshCommand(agent, knownHosts string) string {
	return "ssh -F /dev/null -o IdentitiesOnly=no -o IdentityFile=/dev/null -o BatchMode=yes" +
		" -o " + shellQuote("IdentityAgent="+agent) +
		" -o " + shellQuote("UserKnownHostsFile="+knownHosts) +
		" -o StrictHostKeyChecking=accept-new"
}

func hasVar(env []string, name string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, name+"=") {
			return true
		}
	}
	return false
}

// envScript is env as sh: blank variables unset, the rest exported. A
// login shell sources it after the person's profile. gh's token variables
// are put back from HARNESS_GH_TOKEN, so the file holds no token.
func envScript(env []string) string {
	var b strings.Builder
	b.WriteString("# Written by the harness: the git environment of this session.\n")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch {
		case k == ghTokenEnv:
		case slices.Contains(ghTokenVars, k):
			fmt.Fprintf(&b, "export %s=\"$%s\"\n", k, ghTokenEnv)
		case v == "" && k != "GIT_CONFIG_VALUE_0":
			fmt.Fprintf(&b, "unset %s\n", k)
		default:
			fmt.Fprintf(&b, "export %s=%s\n", k, shellQuote(v))
		}
	}
	return b.String()
}

// shellQuote quotes s for sh.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// writeFile replaces path with content atomically, mode perm.
func writeFile(path, content string, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

package gitcli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitAuthFiles = (*AuthFiles)(nil)

// NoGitHubToken is what gh's token variables hold when no token resolves:
// unset, gh would read the OS keyring, which is the machine's.
const NoGitHubToken = "harness-no-credential"

// The files of a git directory (see "Contract: Harness server ↔ Agent
// sessions — git credentials" §1).
const (
	gitconfigFile = "gitconfig"
	httpsFile     = "https"
	helperFile    = "credential-helper"
	sshKeyFile    = "ssh_key"
	envFile       = "env.sh"
	ghDir         = "gh"
)

// credentialHelper answers git's "get" with the https file's login, only for
// an http(s) request to the file's host (its first line). It is pure sh, so
// nothing of the machine but /bin/sh runs.
const credentialHelper = `#!/bin/sh
# Written by the harness: answers git only for the stored credential's host.
[ "$1" = get ] || exit 0
f="${0%/*}/https"
[ -r "$f" ] || exit 0
proto= host=
while IFS= read -r l; do
	case $l in
	protocol=*) proto=${l#protocol=} ;;
	host=*) host=${l#host=} ;;
	esac
done
case $proto in https|http) ;; *) exit 0 ;; esac
while IFS= read -r l; do
	case $l in
	host=*) [ "${l#host=}" = "$host" ] || exit 0 ;;
	*) printf '%s\n' "$l" ;;
	esac
done <"$f"
`

// identityVars are the identity variables git reads before any config. The
// server's own environment may carry them; sessions get them overwritten.
var identityVars = []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"}

// blanked are machine variables that could hand git or ssh a credential or a
// prompt; every harness git process runs with them empty.
var blanked = []string{"GIT_ASKPASS", "SSH_ASKPASS", "GIT_SSH", "SSH_AUTH_SOCK", "GIT_CONFIG_PARAMETERS", "EMAIL"}

// ghTokenVars are every variable gh reads a token from.
var ghTokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"}

// AuthFiles writes git directories under Root (<data dir>/git): one per
// session under sessions/, one per operation under ops/, and a shared
// known_hosts.
type AuthFiles struct {
	Root string
	// ExcludesFile is the machine's global core.excludesFile path, carried
	// into every gitconfig; "" when it has none. It is a path, not a
	// credential.
	ExcludesFile string
	// Inherited is the server's own environment, which decides which
	// identity variables must be overwritten; nil reads os.Environ.
	Inherited []string
}

// NewAuthFiles returns the adapter writing under root.
func NewAuthFiles(root, excludesFile string) *AuthFiles {
	return &AuthFiles{Root: root, ExcludesFile: excludesFile}
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (a *AuthFiles) sessionDir(sessionID string) (string, error) {
	if !sessionIDPattern.MatchString(sessionID) || strings.Contains(sessionID, "..") {
		return "", fmt.Errorf("git directory: bad session id %q", sessionID)
	}
	return filepath.Join(a.Root, "sessions", sessionID), nil
}

func (a *AuthFiles) knownHosts() string { return filepath.Join(a.Root, "known_hosts") }

// WriteSession writes the session's directory, replacing each file
// atomically so a git command running meanwhile reads the old or the new.
func (a *AuthFiles) WriteSession(sessionID string, m ports.GitAuthMaterial) ([]string, string, error) {
	dir, err := a.sessionDir(sessionID)
	if err != nil {
		return nil, "", err
	}
	env, err := a.write(dir, m)
	if err != nil {
		return nil, "", err
	}
	return env, filepath.Join(dir, envFile), nil
}

func (a *AuthFiles) RemoveSession(sessionID string) error {
	dir, err := a.sessionDir(sessionID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func (a *AuthFiles) SessionIDs() ([]string, error) {
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

func (a *AuthFiles) WriteOperation(m ports.GitAuthMaterial) ([]string, func(), error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, nil, err
	}
	dir := filepath.Join(a.Root, "ops", hex.EncodeToString(b[:]))
	env, err := a.write(dir, m)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}
	return env, func() { _ = os.RemoveAll(dir) }, nil
}

func (a *AuthFiles) SweepOperations() error {
	return os.RemoveAll(filepath.Join(a.Root, "ops"))
}

// write fills dir and answers the environment that points git at it.
func (a *AuthFiles) write(dir string, m ports.GitAuthMaterial) ([]string, error) {
	if err := os.MkdirAll(filepath.Join(dir, ghDir), 0o700); err != nil {
		return nil, err
	}
	for _, d := range []string{a.Root, filepath.Dir(dir), dir, filepath.Join(dir, ghDir)} {
		if err := os.Chmod(d, 0o700); err != nil {
			return nil, err
		}
	}
	if err := writeFile(filepath.Join(dir, gitconfigFile), a.gitconfig(m.Identity), 0o600); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(dir, helperFile), credentialHelper, 0o700); err != nil {
		return nil, err
	}
	if err := writeOrRemove(filepath.Join(dir, httpsFile), httpsLogin(m.HTTPS)); err != nil {
		return nil, err
	}
	key := m.SSHKey.Reveal()
	if key != "" && !strings.HasSuffix(key, "\n") {
		key += "\n" // ssh refuses a key file without its final newline
	}
	if err := writeOrRemove(filepath.Join(dir, sshKeyFile), key); err != nil {
		return nil, err
	}
	env := a.env(dir, m)
	if err := writeFile(filepath.Join(dir, envFile), envScript(env), 0o600); err != nil {
		return nil, err
	}
	return env, nil
}

func httpsLogin(l *ports.GitHTTPSLogin) string {
	if l == nil || l.Token == "" {
		return ""
	}
	// The host first: the helper stops at a host that is not the asked one.
	return "host=" + l.Host + "\nusername=" + l.Username + "\npassword=" + l.Token.Reveal() + "\n"
}

func (a *AuthFiles) gitconfig(id *domain.GitIdentity) string {
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
func (a *AuthFiles) env(dir string, m ports.GitAuthMaterial) []string {
	env := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(dir, gitconfigFile),
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.helper",
		"GIT_CONFIG_VALUE_1=!" + shellQuote(filepath.Join(dir, helperFile)),
		"GIT_TERMINAL_PROMPT=0",
	}
	for _, k := range blanked {
		env = append(env, k+"=")
	}
	env = append(env, "GIT_SSH_COMMAND="+sshCommand(filepath.Join(dir, sshKeyFile), a.knownHosts()),
		"GH_CONFIG_DIR="+filepath.Join(dir, ghDir))
	token := m.GitHubToken.Reveal()
	if token == "" {
		token = NoGitHubToken
	}
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

// sshCommand is ssh with nothing of the machine's: no config, no agent, no
// default key, never a prompt, and the harness's known_hosts.
func sshCommand(identityFile, knownHosts string) string {
	return "ssh -F /dev/null -o IdentitiesOnly=yes -o IdentityAgent=none -o BatchMode=yes" +
		" -o " + shellQuote("IdentityFile="+identityFile) +
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
// login shell sources it after the person's profile.
func envScript(env []string) string {
	var b strings.Builder
	b.WriteString("# Written by the harness: the git environment of this session.\n")
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if v == "" && k != "GIT_CONFIG_VALUE_0" {
			fmt.Fprintf(&b, "unset %s\n", k)
			continue
		}
		fmt.Fprintf(&b, "export %s=%s\n", k, shellQuote(v))
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

// writeOrRemove writes content (mode 0600), or removes path when it is "".
func writeOrRemove(path, content string) error {
	if content == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeFile(path, content, 0o600)
}

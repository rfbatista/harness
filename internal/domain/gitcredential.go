package domain

import (
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Git credentials are what every git remote operation the harness runs
// authenticates with: HTTPS tokens and SSH keys stored in the harness, never
// the machine's own git setup. See "Contract: Harness server ↔ Web UI — git
// credentials" and "Contract: Harness server ↔ Agent sessions — git
// credentials".

// GitCredentialKind is what a credential holds.
type GitCredentialKind string

const (
	GitHTTPSToken GitCredentialKind = "https_token"
	GitSSHKey     GitCredentialKind = "ssh_key"
)

// Valid reports whether k is a known kind.
func (k GitCredentialKind) Valid() bool { return k == GitHTTPSToken || k == GitSSHKey }

// DefaultUsername is the user a credential of kind k sends when none is given.
func (k GitCredentialKind) DefaultUsername() string {
	if k == GitSSHKey {
		return "git"
	}
	return "x-access-token"
}

// GitCredential is a stored credential as every surface shows it: its secret
// is never part of it. Times are UTC, whole seconds, so a page seed and an
// API body encode it byte for byte alike.
type GitCredential struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Kind        GitCredentialKind `json:"kind"`
	Host        string            `json:"host"`
	Username    string            `json:"username"`
	Fingerprint string            `json:"fingerprint"`
	// PublicKey is an ssh_key's OpenSSH public key line; nil for a token.
	PublicKey  *string    `json:"public_key"`
	IsDefault  bool       `json:"is_default"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// GitIdentity is the author of commits made in harness worktrees; one per
// harness.
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// RepositoryCredentialBinding pins a repository to a credential; a nil
// CredentialID resolves automatically.
type RepositoryCredentialBinding struct {
	RepositoryID string  `json:"repository_id"`
	CredentialID *string `json:"credential_id"`
}

// RepositoryCredential is a repository's binding with what a push from it
// would use now, and its origin (userinfo stripped; nil when it has none).
type RepositoryCredential struct {
	Binding  RepositoryCredentialBinding `json:"binding"`
	Resolved *GitCredential              `json:"resolved"`
	// PullRequests is the https token GitHub API calls on the repository
	// would use now; nil when there is none. An ssh_key never serves them.
	PullRequests *GitCredential `json:"pull_requests"`
	RemoteURL    *string        `json:"remote_url"`
}

// GitTime is t as credentials carry it: UTC, whole seconds.
func GitTime(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }

// Secret is a token, private key or passphrase on its way in. Every way of
// printing it — fmt, JSON, slog — prints [redacted]; Reveal is the only way
// to its value.
type Secret string

const redacted = "[redacted]"

// Reveal is the secret's value.
func (s Secret) Reveal() string { return string(s) }

func (s Secret) String() string   { return redacted }
func (s Secret) GoString() string { return redacted }

// LogValue keeps the secret out of structured logs.
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON keeps the secret out of any JSON written from it.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Error codes of the git credentials contract.
const (
	CodeGitCredentialNotFound     = "GIT_CREDENTIAL_NOT_FOUND"
	CodeGitCredentialNameTaken    = "GIT_CREDENTIAL_NAME_TAKEN"
	CodeGitCredentialInvalidKey   = "GIT_CREDENTIAL_INVALID_KEY"
	CodeGitCredentialHostMismatch = "GIT_CREDENTIAL_HOST_MISMATCH"
	CodeGitCredentialKindMismatch = "GIT_CREDENTIAL_KIND_MISMATCH"
	CodeGitCredentialMissing      = "GIT_CREDENTIAL_MISSING"
	CodeGitCredentialRejected     = "GIT_CREDENTIAL_REJECTED"
	CodeGitCredentialStoreLocked  = "GIT_CREDENTIAL_STORE_LOCKED"
)

// GitCredentialError is a coded error of the git credentials context; its
// message starts with the code, as the contract's envelope says.
func GitCredentialError(code, message string) *StructuredError {
	return &StructuredError{Code: code, Message: code + ": " + message}
}

// GitCredentialMissing says what to add for a repository: a credential of
// kind for host.
func GitCredentialMissing(host string, kind GitCredentialKind) error {
	return &DetailedError{
		StructuredError: GitCredentialError(CodeGitCredentialMissing,
			"no stored "+string(kind)+" for "+host+"; add one on the Git credentials page"),
		Details: map[string]any{"host": host, "kind": string(kind)},
	}
}

// GitCredentialRejected says the host refused the stored credential id;
// reason is the host's, never the secret.
func GitCredentialRejected(host, credentialID, reason string) error {
	return &DetailedError{
		StructuredError: GitCredentialError(CodeGitCredentialRejected, host+" refused the stored credential: "+reason),
		Details:         map[string]any{"host": host, "credential_id": credentialID},
	}
}

// ErrGitCredentialStoreLocked is the answer of anything that needs a stored
// secret while the server has no key, or the wrong one, to read them.
var ErrGitCredentialStoreLocked = GitCredentialError(CodeGitCredentialStoreLocked,
	"the server has no key to read stored git credentials; restore HARNESS_SECRET_KEY or the data dir's secret.key")

// NormalizeGitHost is host as credentials store it: lowercase, without
// scheme, user, path or the scheme's default port; "" when nothing is left.
// It accepts what a person pastes: "https://GitHub.com/acme", "git@github.com:acme/x.git",
// "gitlab.example.com:8443".
func NormalizeGitHost(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(s, "://") || strings.Contains(s, "@") {
		if r, ok := ParseGitRemoteURL(s); ok {
			return r.Host
		}
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '@'); i >= 0 {
		s = s[i+1:]
	}
	if h, port, ok := strings.Cut(s, ":"); ok {
		if _, err := strconv.Atoi(port); err != nil || port == "" {
			s = h
		}
	}
	return strings.TrimSuffix(s, ".")
}

// GitRemoteURL is a remote URL as far as credentials care: which kind of
// credential it takes and the host it goes to.
type GitRemoteURL struct {
	// Kind is the credential it authenticates with; "" for a remote that
	// takes none (git://, file://).
	Kind GitCredentialKind
	// Host is lowercase, with a port only when it is not the scheme's default.
	Host string
}

// Takes reports whether a credential of kind serves the remote: the kind its
// scheme names, or, for an https remote without an explicit port, an SSH key
// of the host (the SSH fallback: its SSH port is the default one).
func (r GitRemoteURL) Takes(kind GitCredentialKind) bool {
	switch {
	case r.Kind == "":
		return false
	case r.Kind == kind:
		return true
	}
	return r.Kind == GitHTTPSToken && kind == GitSSHKey && !strings.Contains(r.Host, ":")
}

// UsesSSHFallback reports whether pushing to the remote with c goes over SSH
// although the remote is an https one.
func (r GitRemoteURL) UsesSSHFallback(c *GitCredential) bool {
	return c != nil && r.Kind == GitHTTPSToken && c.Kind == GitSSHKey
}

// SSHFallbackBase is the ssh prefix that replaces "https://host/" while the
// SSH fallback applies: "user@host:". user defaults to git, the user of every
// code host.
func SSHFallbackBase(host, user string) string {
	if user == "" {
		user = "git"
	}
	return user + "@" + host + ":"
}

// SSHFallbackURL is the ssh form of an https origin, "user@host:path", the
// path as it is, including any .git suffix; false when origin is no https URL
// of a host without a port.
func SSHFallbackURL(origin, user string) (string, bool) {
	r, ok := ParseGitRemoteURL(origin)
	if !ok || !r.Takes(GitSSHKey) || r.Kind != GitHTTPSToken {
		return "", false
	}
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil {
		return "", false
	}
	return SSHFallbackBase(u.Hostname(), user) + strings.TrimPrefix(u.Path, "/"), true
}

// ParseGitRemoteURL reads raw as https://host/…, http://host/…,
// ssh://[user@]host[:port]/… or scp-like user@host:path. Local paths and
// file:// are not remotes.
func ParseGitRemoteURL(raw string) (GitRemoteURL, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GitRemoteURL{}, false
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return GitRemoteURL{}, false
		}
		var kind GitCredentialKind
		def := ""
		switch strings.ToLower(u.Scheme) {
		case "https":
			kind, def = GitHTTPSToken, "443"
		case "http":
			kind, def = GitHTTPSToken, "80"
		case "ssh", "git+ssh", "ssh+git":
			kind, def = GitSSHKey, "22"
		case "git":
			def = "9418"
		default:
			return GitRemoteURL{}, false
		}
		host := strings.ToLower(u.Hostname())
		if p := u.Port(); p != "" && p != def {
			host += ":" + p
		}
		return GitRemoteURL{Kind: kind, Host: host}, true
	}
	// scp-like: [user@]host:path, where host has no slash before the colon.
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || strings.ContainsRune(raw[:colon], '/') {
		return GitRemoteURL{}, false
	}
	host := raw[:colon]
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	if host == "" || len(host) == 1 { // "C:\…" is a Windows path
		return GitRemoteURL{}, false
	}
	return GitRemoteURL{Kind: GitSSHKey, Host: strings.ToLower(host)}, true
}

// StripURLUserinfo is raw without the user and password an https or ssh
// URL may carry, so it can be shown or put in an error message. scp-like
// "git@host:path" keeps its user, which is no secret.
func StripURLUserinfo(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		// Unparseable: cut anything between the scheme and the last '@' of
		// the authority so a token can never get through.
		scheme, rest, _ := strings.Cut(raw, "://")
		authority, path, _ := strings.Cut(rest, "/")
		if i := strings.LastIndexByte(authority, '@'); i >= 0 {
			authority = authority[i+1:]
		}
		if path != "" {
			return scheme + "://" + authority + "/" + path
		}
		return scheme + "://" + authority
	}
	u.User = nil
	return u.String()
}

// ResolveGitCredential is the credential a remote operation on remote uses
// (§4 of the Web UI contract): the pinned one when it exists; otherwise the
// default credential of remote's kind for its host, or the only one, then
// (an https remote without a port) the host's default SSH key, or its only
// one; nil when nothing resolves. remote nil (no origin) resolves only a pin.
func ResolveGitCredential(creds []*GitCredential, pinnedID string, remote *GitRemoteURL) *GitCredential {
	if pinnedID != "" {
		for _, c := range creds {
			if c.ID == pinnedID {
				return c
			}
		}
	}
	if remote == nil || remote.Kind == "" {
		return nil
	}
	if c := CredentialFor(creds, remote.Host, remote.Kind); c != nil {
		return c
	}
	if remote.Kind != GitSSHKey && remote.Takes(GitSSHKey) {
		return CredentialFor(creds, remote.Host, GitSSHKey)
	}
	return nil
}

// GitHubCredential is the https token GitHub API calls use: the push
// credential when it is a token of apiHost, else the host's default token or
// its only one. apiHost is the remote's host, or github.com with no remote.
func GitHubCredential(creds []*GitCredential, push *GitCredential, remote *GitRemoteURL) *GitCredential {
	apiHost := "github.com"
	if remote != nil {
		apiHost = remote.Host
	}
	if push != nil && push.Kind == GitHTTPSToken && push.Host == apiHost {
		return push
	}
	return CredentialFor(creds, apiHost, GitHTTPSToken)
}

// CredentialFor is the default credential of kind for host, or the only one;
// nil when there is none or several without a default.
func CredentialFor(creds []*GitCredential, host string, kind GitCredentialKind) *GitCredential {
	var only *GitCredential
	n := 0
	for _, c := range creds {
		if c.Host != host || c.Kind != kind {
			continue
		}
		if c.IsDefault {
			return c
		}
		only = c
		n++
	}
	if n == 1 {
		return only
	}
	return nil
}

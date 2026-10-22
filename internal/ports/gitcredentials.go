package ports

import (
	"context"

	"operators-mcp/internal/domain"
)

// The git credentials context: the HTTPS tokens, SSH keys and commit identity
// every git remote operation of the harness uses. See "Contract: Harness
// server ↔ Web UI — git credentials". Secrets go in and never come out.

// CreateGitCredential is a new credential. An https_token needs Token; an
// ssh_key needs PrivateKey (with Passphrase when it is encrypted) or Generate.
type CreateGitCredential struct {
	Name       string                   `json:"name"`
	Kind       domain.GitCredentialKind `json:"kind"`
	Host       string                   `json:"host"`
	Username   string                   `json:"username,omitempty"`
	Token      domain.Secret            `json:"token,omitempty"`
	PrivateKey domain.Secret            `json:"private_key,omitempty"`
	Passphrase domain.Secret            `json:"passphrase,omitempty"`
	Generate   bool                     `json:"generate,omitempty"`
	IsDefault  bool                     `json:"is_default,omitempty"`
}

// UpdateGitCredential changes a credential. Nil fields and empty secrets keep
// their value; the kind never changes.
type UpdateGitCredential struct {
	CredentialID string        `json:"credential_id"`
	Name         *string       `json:"name,omitempty"`
	Host         *string       `json:"host,omitempty"`
	Username     *string       `json:"username,omitempty"`
	Token        domain.Secret `json:"token,omitempty"`
	PrivateKey   domain.Secret `json:"private_key,omitempty"`
	Passphrase   domain.Secret `json:"passphrase,omitempty"`
	IsDefault    *bool         `json:"is_default,omitempty"`
}

// CarriesSecret reports whether the update replaces a secret.
func (u UpdateGitCredential) CarriesSecret() bool { return u.Token != "" || u.PrivateKey != "" }

// GitCredentialTest is what testing a credential found. A host refusal is
// OK false, never an error.
type GitCredentialTest struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	CheckedURL string `json:"checked_url"`
}

// GitCredentialChange is one change on the catalog feed: a credential as it
// is after create, update or test, its deletion (only the id is set), or the
// identity after an update. Exactly one of Credential and Identity is set.
type GitCredentialChange struct {
	Credential *domain.GitCredential
	Identity   *domain.GitIdentity
	Deleted    bool
}

// GitCredentialList is every credential, sorted by host then name, and
// whether the store is locked: its key is missing or is not the one that
// sealed the secrets. Locked is known without decrypting anything.
type GitCredentialList struct {
	Credentials []*domain.GitCredential `json:"credentials"`
	Locked      bool                    `json:"locked"`
}

// GitCredentials is the driving port of the git credentials context:
// *gitcredentials.Service satisfies it, and httpclient implements it over
// the wire. Its reads return the §1 shapes, so the templ BFF seeds encode
// byte for byte like the API bodies.
type GitCredentials interface {
	// ListGitCredentials is the list as list_git_credentials answers it;
	// Credentials is never nil.
	ListGitCredentials(ctx context.Context) (*GitCredentialList, error)
	CreateGitCredential(ctx context.Context, in CreateGitCredential) (*domain.GitCredential, error)
	UpdateGitCredential(ctx context.Context, in UpdateGitCredential) (*domain.GitCredential, error)
	// DeleteGitCredential unpins the repositories pinned to it.
	DeleteGitCredential(ctx context.Context, credentialID string) error
	// TestGitCredential checks the credential against repositoryID's origin,
	// or against its host when repositoryID is "".
	TestGitCredential(ctx context.Context, credentialID, repositoryID string) (*GitCredentialTest, error)

	// GetGitIdentity is nil while none is set.
	GetGitIdentity(ctx context.Context) (*domain.GitIdentity, error)
	UpdateGitIdentity(ctx context.Context, name, email string) (*domain.GitIdentity, error)

	GetRepositoryCredential(ctx context.Context, repositoryID string) (*domain.RepositoryCredential, error)
	// ListRepositoryCredentials is in the project's repository order; never
	// nil.
	ListRepositoryCredentials(ctx context.Context, projectID string) ([]*domain.RepositoryCredential, error)
	// SetRepositoryCredential pins credentialID, or unpins with nil.
	SetRepositoryCredential(ctx context.Context, repositoryID string, credentialID *string) (*domain.RepositoryCredential, error)

	// FollowGitCredentials delivers each change until ctx ends. The channel
	// closes when ctx ends or the follower fell behind.
	FollowGitCredentials(ctx context.Context) (<-chan GitCredentialChange, error)
}

// StoredGitCredential is a credential with its sealed secret and the id of
// the key that sealed it.
type StoredGitCredential struct {
	Credential domain.GitCredential
	Sealed     []byte
	KeyID      string
}

// GitCredentialRepository keeps credentials (driven).
type GitCredentialRepository interface {
	List(ctx context.Context) ([]*StoredGitCredential, error)
	// Get is nil, nil when there is no such credential.
	Get(id string) (*StoredGitCredential, error)
	// Create stores c and assigns its id.
	Create(c *StoredGitCredential) (*StoredGitCredential, error)
	Update(c *StoredGitCredential) error
	Delete(id string) error
}

// GitIdentityRepository keeps the one identity (driven).
type GitIdentityRepository interface {
	// Get is nil, nil while none is set.
	Get() (*domain.GitIdentity, error)
	Put(id domain.GitIdentity) error
}

// RepositoryCredentialPin is one repository pinned to a credential.
type RepositoryCredentialPin struct {
	RepositoryID string
	ProjectID    string
	CredentialID string
}

// RepositoryCredentialRepository keeps repositories' pins (driven). A
// repository with no row resolves automatically.
type RepositoryCredentialRepository interface {
	// Get is "" when the repository is not pinned.
	Get(repositoryID string) (string, error)
	// ListByRepositories is the pins of any of repositoryIDs, read at once;
	// a repository not pinned has none.
	ListByRepositories(ctx context.Context, repositoryIDs []string) ([]RepositoryCredentialPin, error)
	Put(pin RepositoryCredentialPin) error
	Delete(repositoryID string) error
	// ListByCredential is every pin on credentialID.
	ListByCredential(credentialID string) ([]RepositoryCredentialPin, error)
	DeleteByCredential(credentialID string) error
	DeleteByProject(projectID string) error
}

// SecretBox seals secrets at rest with a key held outside the database
// (driven).
type SecretBox interface {
	// KeyID names the key the box holds; "" when it has none yet.
	KeyID() string
	// Seal encrypts plain with the box's key. With create, a box that has
	// no key makes one first; without, it answers GIT_CREDENTIAL_STORE_LOCKED.
	Seal(plain []byte, create bool) (sealed []byte, keyID string, err error)
	// Open decrypts what Seal sealed with the same key, or answers
	// GIT_CREDENTIAL_STORE_LOCKED.
	Open(sealed []byte) ([]byte, error)
}

// OriginReader reads a checkout's origin URL (driven); "" when it has none.
// gitcli.BranchPublisher satisfies it.
type OriginReader interface {
	OriginURL(ctx context.Context, repoRoot string) (string, error)
}

// GitHTTPSLogin is what the credential socket answers for one host.
type GitHTTPSLogin struct {
	Host     string
	Username string
	Token    domain.Secret
}

// GitAuthMaterial is what one git directory is served: the identity, the login
// for an https origin, the private key for an ssh one, and the token gh
// gets. Nil or empty parts are left out, so git runs without them.
type GitAuthMaterial struct {
	Identity    *domain.GitIdentity
	HTTPS       *GitHTTPSLogin
	SSHKey      domain.Secret
	GitHubToken domain.Secret
	// SSHFallback is set while SSHKey serves an https origin: git then goes
	// over ssh to the host instead of https. The checkout's remote is left
	// alone.
	SSHFallback *GitSSHFallback
}

// GitSSHFallback is the host whose https URLs go over ssh, as User.
type GitSSHFallback struct {
	Host string
	User string
}

// GitAuthSource reads a git directory's material from the store. The
// directory's sockets call it on every request, so what git gets is what is
// stored at that moment.
type GitAuthSource func() (GitAuthMaterial, error)

// GitAuthBroker keeps harness-owned git directories outside every worktree
// and serves them their secrets (driven): each directory holds a gitconfig
// and an env file, never a key or a token; git and ssh reach those through
// the directory's sockets — an ssh-agent and a credential-cache — which read
// its source on every request. The environment it answers makes git and gh
// use them and nothing of the machine's. See "Contract: Harness server ↔
// Agent sessions — git credentials" §1–§2.
type GitAuthBroker interface {
	// OpenSession writes the session's directory and serves it from src,
	// or, when it is open, rewrites its files and swaps src in place. Its
	// paths, so its env, never change for the session.
	OpenSession(sessionID string, src GitAuthSource) (env []string, envFile string, err error)
	CloseSession(sessionID string) error
	// SessionIDs are the sessions that have a directory.
	SessionIDs() ([]string, error)
	// OpenOperation opens a directory for one server-side operation;
	// release closes and removes it.
	OpenOperation(src GitAuthSource) (env []string, release func(), err error)
	// SweepOperations removes what a previous run left of operations.
	SweepOperations() error
}

// GitCredentialSecret is a credential with its secret: the token, or the
// unencrypted private key. Only the adapters that hand it to git see it.
type GitCredentialSecret struct {
	domain.GitCredential
	Secret domain.Secret
}

// GitCredentialTester checks a credential without changing anything
// (driven): against remoteURL with ls-remote, or, when it is "", against the
// credential's host. A host refusal or an unreachable host is OK false,
// never an error.
type GitCredentialTester interface {
	Test(ctx context.Context, c GitCredentialSecret, remoteURL string) (*GitCredentialTest, error)
}

// GitResolution is what a repository's remote operations would use now,
// read from the store alone.
type GitResolution struct {
	// Remote is origin as credentials see it; nil when there is no origin or
	// it is no remote URL.
	Remote *domain.GitRemoteURL
	// Push authenticates fetch and push to origin; nil when none resolves.
	Push *domain.GitCredential
	// GitHub is the https_token for origin's host, for GitHub API calls; nil
	// when none resolves.
	GitHub *domain.GitCredential
}

// GitOperation is one server-side remote operation's credentials: Env for
// git, and the GitHub token for API calls, which never reaches a process.
// Release closes its directory.
type GitOperation struct {
	Env         []string
	Push        *domain.GitCredential
	GitHub      *domain.GitCredential
	GitHubToken domain.Secret
	Release     func()
}

// SessionGit is what a session's processes get: Env on top of their own,
// EnvFile for login shells to source after the profile, and Note, a line for
// the session's brief when its git cannot authenticate ("" when it can).
type SessionGit struct {
	Env     []string
	EnvFile string
	Note    string
}

// GitAuth is how other contexts get a repository's stored credentials
// (read port; *gitcredentials.Service satisfies it). Nothing resolving is an
// answer, never a reason to use the machine's.
type GitAuth interface {
	// Resolve reads the store only: no git, no files.
	Resolve(ctx context.Context, repositoryID, originURL string) (*GitResolution, error)
	// Operation opens the credentials of one operation on the repository;
	// GIT_CREDENTIAL_STORE_LOCKED when they cannot be read.
	Operation(ctx context.Context, repositoryID, originURL string) (*GitOperation, error)
	// SessionEnv opens (or reopens) the session's git directory. A locked
	// store or nothing resolving still answers, with a Note.
	SessionEnv(ctx context.Context, sessionID, repositoryID string) (*SessionGit, error)
	// GitHubToken is the https_token for origin's host and its secret, read
	// in memory for API calls: no files. Nil when none resolves;
	// GIT_CREDENTIAL_STORE_LOCKED when it cannot be read.
	GitHubToken(ctx context.Context, repositoryID, originURL string) (*domain.GitCredential, domain.Secret, error)
	// EndSession closes and removes the session's directory.
	EndSession(sessionID string)
	// Used records that a credential just authenticated.
	Used(credentialID string)
}

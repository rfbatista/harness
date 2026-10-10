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

// GitCredentials is the driving port of the git credentials context:
// *gitcredentials.Service satisfies it, and httpclient implements it over
// the wire. Its reads return the §1 shapes, so the templ BFF seeds encode
// byte for byte like the API bodies.
type GitCredentials interface {
	// ListGitCredentials is sorted by host, then name; never nil.
	ListGitCredentials(ctx context.Context) ([]*domain.GitCredential, error)
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
	List() ([]*StoredGitCredential, error)
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

// GitHTTPSLogin is what the credential helper answers for one host.
type GitHTTPSLogin struct {
	Host     string
	Username string
	Token    domain.Secret
}

// GitAuthMaterial is what one git directory holds: the identity, the login
// for an https origin, the private key for an ssh one, and the token gh
// gets. Nil or empty parts are left out, so git runs without them.
type GitAuthMaterial struct {
	Identity    *domain.GitIdentity
	HTTPS       *GitHTTPSLogin
	SSHKey      domain.Secret
	GitHubToken domain.Secret
}

// GitAuthFiles writes harness-owned git directories — gitconfig, credential
// helper, key, env file — outside every worktree, and answers the
// environment that makes git and gh use them and nothing of the machine's
// (driven). See "Contract: Harness server ↔ Agent sessions — git
// credentials" §1–§2.
type GitAuthFiles interface {
	// WriteSession writes, or rewrites in place, the session's directory.
	// Its paths, so its env, never change for the session.
	WriteSession(sessionID string, m GitAuthMaterial) (env []string, envFile string, err error)
	RemoveSession(sessionID string) error
	// SessionIDs are the sessions that have a directory.
	SessionIDs() ([]string, error)
	// WriteOperation writes a directory for one server-side operation;
	// release removes it.
	WriteOperation(m GitAuthMaterial) (env []string, release func(), err error)
	// SweepOperations removes the operation directories a previous run left.
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
// Release removes its files.
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
	// Operation writes the credentials of one operation on the repository;
	// GIT_CREDENTIAL_STORE_LOCKED when they cannot be read.
	Operation(ctx context.Context, repositoryID, originURL string) (*GitOperation, error)
	// SessionEnv writes (or rewrites) the session's git directory. A locked
	// store or nothing resolving still answers, with a Note.
	SessionEnv(ctx context.Context, sessionID, repositoryID string) (*SessionGit, error)
	// EndSession removes the session's directory.
	EndSession(sessionID string)
	// Used records that a credential just authenticated.
	Used(credentialID string)
}

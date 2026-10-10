// What the credentials module needs from the outside world. Implemented by
// infrastructure/credentials-gateway.js (the Web UI contract "git
// credentials": /api/*git_credential*, /api/*repository_credential* and the
// catalog feed /api/project_events) and infrastructure/memory-gateway.js
// (tests and web/dev). Both run testing/gateway-contract.js.
//
// Secrets travel only in the inputs of create and update. No answer, change
// or seed carries one back.

/**
 * @typedef {import("./credential.js").Credential} Credential
 * @typedef {import("./credential.js").Identity} Identity
 * @typedef {import("./credential.js").Kind} Kind
 * @typedef {import("./credential.js").RepositoryCredential} RepositoryCredential
 * @typedef {import("./credential.js").TestResult} TestResult
 * @typedef {import("./credential.js").Change} Change
 * @typedef {import("../../../shared/domain/feed.js").FeedStatus} FeedStatus
 *
 * @typedef {object} CreateInput
 * @property {string} name
 * @property {Kind} kind
 * @property {string} host
 * @property {string} [username]    "" or absent: x-access-token for a token, git for a key
 * @property {string} [token]       https_token: required
 * @property {string} [privateKey]  ssh_key: OpenSSH or PEM text, unless generate
 * @property {string} [passphrase]  only to open an encrypted privateKey; never stored
 * @property {boolean} [generate]   ssh_key: the server makes an ed25519 key
 * @property {boolean} [isDefault]
 *
 * @typedef {object} UpdateInput  absent fields, and empty secrets, keep their value
 * @property {string} credentialId
 * @property {string} [name]
 * @property {string} [host]
 * @property {string} [username]
 * @property {string} [token]
 * @property {string} [privateKey]
 * @property {string} [passphrase]
 * @property {boolean} [isDefault]
 *
 * @typedef {object} PageSeed  the Git credentials page's first paint
 * @property {Credential[]} credentials
 * @property {Identity|null} identity
 *
 * @typedef {object} RepositoriesSeed  the Repositories page's first paint
 * @property {string} projectId
 * @property {Credential[]} credentials
 * @property {RepositoryCredential[]} repositories
 *
 * @typedef {object} CredentialGateway
 * @property {() => Promise<Credential[]>} list
 *           By host, then name. Answers while the store is locked.
 * @property {(input: CreateInput) => Promise<Credential>} create
 *           Rejects with INVALID_INPUT (no name, host or token),
 *           GIT_CREDENTIAL_NAME_TAKEN, GIT_CREDENTIAL_INVALID_KEY, or
 *           GIT_CREDENTIAL_STORE_LOCKED.
 * @property {(input: UpdateInput) => Promise<Credential>} update
 *           The kind never changes. Rejects with GIT_CREDENTIAL_NOT_FOUND,
 *           NAME_TAKEN, INVALID_KEY, HOST_MISMATCH (a host change on a pinned
 *           credential; details.repositories: [{id, name}]), or STORE_LOCKED
 *           when it carries a secret.
 * @property {(credentialId: string) => Promise<void>} remove
 *           Repositories pinned to it go back to automatic. Rejects with
 *           GIT_CREDENTIAL_NOT_FOUND.
 * @property {(credentialId: string, repositoryId?: string) => Promise<TestResult>} test
 *           Signs in without changing anything. A host that refuses or cannot
 *           be reached is ok:false, never an error. Rejects with NOT_FOUND,
 *           REPOSITORY_NOT_FOUND or STORE_LOCKED.
 * @property {() => Promise<Identity|null>} getIdentity
 * @property {(identity: Identity) => Promise<Identity>} updateIdentity
 *           Rejects with INVALID_INPUT.
 * @property {(repositoryId: string) => Promise<RepositoryCredential>} getRepositoryCredential
 * @property {(projectId: string) => Promise<RepositoryCredential[]>} listRepositoryCredentials
 *           In the project's repository order. Rejects with PROJECT_NOT_FOUND.
 * @property {(repositoryId: string, credentialId: string|null) => Promise<RepositoryCredential>} setRepositoryCredential
 *           null goes back to automatic. Rejects with REPOSITORY_NOT_FOUND,
 *           GIT_CREDENTIAL_NOT_FOUND, HOST_MISMATCH or KIND_MISMATCH.
 * @property {(seed: unknown) => PageSeed} decodePageSeed
 *           Throws BAD_RESPONSE when malformed.
 * @property {(seed: unknown) => RepositoriesSeed} decodeRepositoriesSeed
 *           Throws BAD_RESPONSE when malformed.
 * @property {(onChange: (change: Change) => void, onStatus?: (status: FeedStatus) => void) => () => void} follow
 *           Every credential created, updated, tested or deleted and every
 *           identity saved, anywhere, in order, the person's own included
 *           (possibly before their request answers). Bindings are not on it.
 *           Returns the function that stops it.
 */

export {};

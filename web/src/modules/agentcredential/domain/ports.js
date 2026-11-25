// What the agent credential module needs from the outside world: the
// credential agents sign in with (ADR 0018). Implemented by
// infrastructure/agent-credential-gateway.js over /api/*_agent_credential.
// The value goes in through set and never comes back: every call answers
// the status only.

/**
 * @typedef {"oauth" | "api_key" | "login"} CredentialKind  "login" is a project login (ADR 0102).
 *
 * @typedef {object} CredentialStatus
 * @property {boolean} set
 * @property {CredentialKind | ""} kind  "" when not set.
 * @property {string} updated  ISO time it was saved; meaningful only when set.
 * @property {string} [account]  A login's account; "" otherwise.
 * @property {string} [subscription]
 * @property {EffectiveCredential} [effective]  What the project's next claude process runs with.
 *
 * @typedef {object} EffectiveCredential
 * @property {"project" | "global_login" | "global_pasted" | "none"} source
 * @property {CredentialKind | ""} kind
 * @property {string} account  Filled for a login only.
 * @property {string} subscription
 *
 * @typedef {object} AgentCredentialGateway
 * @property {() => Promise<CredentialStatus>} get
 * @property {(kind: CredentialKind, value: string) => Promise<CredentialStatus>} set
 *           Replaces any credential. Rejects with INVALID_INPUT, whose message says why.
 * @property {() => Promise<CredentialStatus>} remove
 * @property {(projectId: string) => Promise<CredentialStatus>} getForProject
 *           The credential the project keeps for itself; the global one is not counted.
 * @property {(projectId: string, kind: CredentialKind, value: string) => Promise<CredentialStatus>} setForProject
 *           Replaces the project's own credential. Rejects with INVALID_INPUT, whose message says why.
 * @property {(projectId: string) => Promise<CredentialStatus>} removeForProject
 *           The project's sessions fall back to the global credential.
 * @property {() => Promise<OAuthState>} oauthState  The sign-in flow as it is now.
 * @property {() => Promise<OAuthState>} oauthStart  Rejects with AGENT_OAUTH_BUSY while one runs.
 * @property {(code: string) => Promise<OAuthState>} oauthSubmit
 * @property {() => Promise<OAuthState>} oauthCancel  Kills the process and clears the state.
 * @property {(projectId: string) => Promise<OAuthState>} projectOAuthState
 *           The project's own sign-in flow as it is now; flows are per project.
 * @property {(projectId: string) => Promise<OAuthState>} projectOAuthStart  Rejects with AGENT_OAUTH_BUSY while this project's runs.
 * @property {(projectId: string, code: string) => Promise<OAuthState>} projectOAuthSubmit
 * @property {(projectId: string) => Promise<OAuthState>} projectOAuthCancel  Kills the process and clears the state.
 * @property {(projectId: string) => Promise<AuthStatus>} projectAuthStatus  `claude auth status` in the project Claude home.
 * @property {(projectId: string) => Promise<AuthStatus>} projectSignOut
 *           Runs `claude auth logout` in the project Claude home; running sessions are not stopped.
 * @property {() => Promise<AuthStatus>} authStatus  `claude auth status` as it is now.
 * @property {() => Promise<AuthStatus>} signOut  Runs `claude auth logout`, answers the status after.
 *
 * @typedef {object} AuthStatus  What `claude auth status` printed; never a token.
 * @property {boolean} signed_in
 * @property {string} account  The email, "" when claude prints none.
 * @property {string} subscription
 * @property {string} auth_method
 * @property {boolean} checked_ok  False when the status could not be read.
 *
 * @typedef {"idle" | "starting" | "waiting_for_code" | "checking" | "done" | "failed"} OAuthPhase
 *
 * @typedef {object} OAuthState  Never carries the token or the pasted code.
 * @property {OAuthPhase} phase
 * @property {string} url  The authorization page; "" until claude prints it.
 * @property {number} remaining_seconds
 * @property {string} message  Why the flow failed.
 * @property {string} detail  The last line the process printed when it died.
 */

export {};

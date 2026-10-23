// What the agent credential module needs from the outside world: the
// credential agents sign in with (ADR 0018). Implemented by
// infrastructure/agent-credential-gateway.js over /api/*_agent_credential.
// The value goes in through set and never comes back: every call answers
// the status only.

/**
 * @typedef {"oauth" | "api_key"} CredentialKind
 *
 * @typedef {object} CredentialStatus
 * @property {boolean} set
 * @property {CredentialKind | ""} kind  "" when not set.
 * @property {string} updated  ISO time it was saved; meaningful only when set.
 *
 * @typedef {object} AgentCredentialGateway
 * @property {() => Promise<CredentialStatus>} get
 * @property {(kind: CredentialKind, value: string) => Promise<CredentialStatus>} set
 *           Replaces any credential. Rejects with INVALID_INPUT, whose message says why.
 * @property {() => Promise<CredentialStatus>} remove
 * @property {() => Promise<OAuthState>} oauthState  The sign-in flow as it is now.
 * @property {() => Promise<OAuthState>} oauthStart  Rejects with AGENT_OAUTH_BUSY while one runs.
 * @property {(code: string) => Promise<OAuthState>} oauthSubmit
 * @property {() => Promise<OAuthState>} oauthCancel  Kills the process and clears the state.
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

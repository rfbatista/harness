// Git credentials: what the harness authenticates git remotes and GitHub with,
// stored in the harness and never read from the machine's own git, ssh or gh
// setup. An HTTPS token or an SSH key belongs to one host; a repository uses
// the one it is pinned to, else the one its origin resolves to. Secrets are
// write-only: nothing here ever holds a token, a private key or a passphrase.
// Mirrors the Web UI contract "git credentials" (rev 3).

/**
 * @typedef {"https_token"|"ssh_key"} Kind
 *
 * @typedef {object} Credential
 * @property {string} id
 * @property {string} name          the person's label, unique ignoring case
 * @property {Kind} kind
 * @property {string} host          lowercase, no scheme or path, an optional :port
 * @property {string} username      the login sent with the token, or the ssh user
 * @property {string} fingerprint   "…abcd" for a token, "SHA256:…" for a key
 * @property {string|null} publicKey  ssh_key only: the OpenSSH line to give the host
 * @property {boolean} isDefault    at most one per (host, kind)
 * @property {Date} createdAt
 * @property {Date} updatedAt
 * @property {Date|null} lastUsedAt
 *
 * @typedef {{ name: string, email: string }} Identity  the author of commits made in harness worktrees
 *
 * @typedef {object} RepositoryCredential  which credential a repository's pushes use
 * @property {string} repositoryId
 * @property {string|null} credentialId   its pin; null resolves automatically
 * @property {Credential|null} resolved   what a push would use now; null when nothing does
 * @property {string|null} remoteUrl      origin without userinfo; null when there is none
 *
 * @typedef {object} TestResult
 * @property {boolean} ok
 * @property {string} message      the server's words, shown as they are
 * @property {string} checkedUrl
 *
 * @typedef {{ kind: "upsert", credential: Credential }
 *         | { kind: "deleted", id: string }
 *         | { kind: "identity", identity: Identity }} Change
 */

export const KINDS = Object.freeze(["https_token", "ssh_key"]);

/** The usernames the server fills in when none is given. */
export const DEFAULT_USERNAME = Object.freeze({ https_token: "x-access-token", ssh_key: "git" });

/** "HTTPS token", "SSH key". */
export const kindLabel = (kind) => (kind === "ssh_key" ? "SSH key" : "HTTPS token");

/**
 * A host as the server stores it: lowercase, without scheme, userinfo or
 * path, keeping a :port. "" when nothing is left.
 */
export function normalizeHost(input) {
  let host = String(input ?? "").trim().toLowerCase();
  host = host.replace(/^[a-z][a-z0-9+.-]*:\/\//, "");
  host = host.replace(/[/?#].*$/, "");
  host = host.replace(/^[^@]*@/, "");
  return host;
}

/**
 * What an origin URL needs: its host and the kind of credential that fits
 * its scheme. null for a local or unreadable one.
 *   https://github.com/o/r.git      → { host: "github.com", kind: "https_token" }
 *   git@github.com:o/r.git          → { host: "github.com", kind: "ssh_key" }
 *   ssh://git@host:2222/o/r.git     → { host: "host:2222", kind: "ssh_key" }
 * @returns {{ host: string, kind: Kind } | null}
 */
export function remoteOf(url) {
  const u = String(url ?? "").trim();
  if (!u) return null;
  const scheme = /^([a-z][a-z0-9+.-]*):\/\//i.exec(u);
  if (scheme) {
    const s = scheme[1].toLowerCase();
    const kind = s === "https" || s === "http" ? "https_token" : s === "ssh" || s === "git+ssh" ? "ssh_key" : null;
    const host = normalizeHost(u);
    return kind && host ? { host, kind } : null;
  }
  // scp-like: [user@]host:path, where host has no slash before the colon.
  const scp = /^(?:[^@/]+@)?([^:/]+):(?!\/\/)/.exec(u);
  if (scp) return { host: scp[1].toLowerCase(), kind: "ssh_key" };
  return null;
}

const byHostThenName = (a, b) => a.host.localeCompare(b.host) || a.name.toLowerCase().localeCompare(b.name.toLowerCase()) || a.id.localeCompare(b.id);

/** Sorted as the server lists them: by host, then name. */
export const sorted = (credentials) => [...credentials].sort(byHostThenName);

/**
 * The credentials, one group per host in the list's order.
 * @param {Credential[]} credentials
 * @returns {{ host: string, credentials: Credential[] }[]}
 */
export function groupByHost(credentials) {
  const groups = [];
  for (const c of sorted(credentials)) {
    const last = groups.at(-1);
    if (last?.host === c.host) last.credentials.push(c);
    else groups.push({ host: c.host, credentials: [c] });
  }
  return groups;
}

/**
 * What a repository with this origin can be pinned to: the credentials of
 * the origin's host and of the kind its scheme takes. Without an origin,
 * any (the server checks the pin once an origin exists).
 * @param {Credential[]} credentials
 * @param {string|null} remoteUrl
 */
export function candidatesFor(credentials, remoteUrl) {
  if (!remoteUrl) return sorted(credentials);
  const remote = remoteOf(remoteUrl);
  if (!remote) return [];
  return sorted(credentials.filter((c) => c.host === remote.host && c.kind === remote.kind));
}

/**
 * What an origin resolves to without a pin (the contract's §4): the default
 * credential of its host and kind, else the only one; null when none fits.
 * @param {Credential[]} credentials
 * @param {string|null} remoteUrl
 */
export function automaticFor(credentials, remoteUrl) {
  const remote = remoteOf(remoteUrl);
  if (!remote) return null;
  const fits = credentials.filter((c) => c.host === remote.host && c.kind === remote.kind);
  return fits.find((c) => c.isDefault) ?? (fits.length === 1 ? fits[0] : null);
}

/**
 * A list after one change: upserted by id, or removed. Identity changes
 * leave it as it is. The default flag is one per (host, kind), so a new
 * default clears it on the others, as the server does.
 * @param {Credential[]} list
 * @param {Change} change
 */
export function applyChange(list, change) {
  if (change.kind === "deleted") return list.filter((c) => c.id !== change.id);
  if (change.kind !== "upsert") return list;
  const next = change.credential;
  const rest = list.filter((c) => c.id !== next.id).map((c) => (next.isDefault && c.isDefault && c.host === next.host && c.kind === next.kind ? Object.freeze({ ...c, isDefault: false }) : c));
  return sorted([...rest, next]);
}

/** Why an identity cannot be saved, as the server would say; "" when it can. */
export function identityProblem({ name, email }) {
  if (!String(name ?? "").trim()) return "Give the name commits are signed with.";
  const e = String(email ?? "").trim();
  if (!e) return "Give the email commits are signed with.";
  if (e.split("@").length !== 2) return "An email has exactly one @.";
  return "";
}

/**
 * Why a credential form cannot be sent yet, before asking the server; ""
 * when it can. secret is whether a secret was given (the form never hands
 * the secret itself to the domain).
 * @param {{ mode: "add"|"edit"|"secret", kind: Kind, name: string, host: string, keySource?: "generate"|"file"|"paste", secret: boolean }} form
 */
export function formProblem({ mode, kind, name, host, keySource = "generate", secret }) {
  if (mode !== "secret") {
    if (!String(name ?? "").trim()) return "Give it a name.";
    if (!normalizeHost(host)) return "Give the host it signs in to, like github.com.";
  }
  if (mode === "add" && kind === "https_token" && !secret) return "Paste the token.";
  if (mode === "add" && kind === "ssh_key" && keySource !== "generate" && !secret) return "Choose or paste the private key.";
  if (mode === "secret" && !secret) return kind === "ssh_key" ? "Choose or paste the new private key." : "Paste the new token.";
  return "";
}

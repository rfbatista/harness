// Git credentials for tests and web/dev pages. The secrets here are fakes;
// tests use them to prove that no secret ever comes back.

export const C0 = new Date("2026-10-10T18:00:00Z");

/** A token value tests search the DOM and every answer for. */
export const SECRET_TOKEN = "ghp_secretTokenNeverShown1234";
/** A private key the fake gateway opens (an unencrypted one). */
export const PRIVATE_KEY = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ\n-----END OPENSSH PRIVATE KEY-----";
/** An encrypted one: it needs memory-gateway's PASSPHRASE. */
export const ENCRYPTED_KEY = "-----BEGIN OPENSSH PRIVATE KEY-----\nENCRYPTED aes256-ctr\n-----END OPENSSH PRIVATE KEY-----";

/** @returns {import("../domain/credential.js").Credential} */
export function makeCredential(overrides = {}) {
  return Object.freeze({
    id: "gc-work",
    name: "work-token",
    kind: "https_token",
    host: "github.com",
    username: "x-access-token",
    fingerprint: "…1234",
    publicKey: null,
    isDefault: true,
    createdAt: C0,
    updatedAt: C0,
    lastUsedAt: null,
    ...overrides,
  });
}

/** An SSH key credential. */
export const makeKey = (overrides = {}) =>
  makeCredential({
    id: "gc-key",
    name: "laptop-key",
    kind: "ssh_key",
    username: "git",
    fingerprint: "SHA256:abcdef",
    publicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexample harness",
    isDefault: false,
    ...overrides,
  });

/** The wire format of a credential. */
export function credentialDTO(c) {
  return {
    id: c.id,
    name: c.name,
    kind: c.kind,
    host: c.host,
    username: c.username,
    fingerprint: c.fingerprint,
    public_key: c.publicKey,
    is_default: c.isDefault,
    created_at: c.createdAt.toISOString(),
    updated_at: c.updatedAt.toISOString(),
    last_used_at: c.lastUsedAt ? c.lastUsedAt.toISOString() : null,
  };
}

/** The wire format of what a repository resolves to. */
export function repositoryCredentialDTO(rc) {
  return {
    binding: { repository_id: rc.repositoryId, credential_id: rc.credentialId },
    resolved: rc.resolved ? credentialDTO(rc.resolved) : null,
    pull_requests: rc.pullRequests ? credentialDTO(rc.pullRequests) : null,
    remote_url: rc.remoteUrl,
  };
}

/** Repositories of project p1 (and one of p2) with their origins. */
export const REPOSITORIES = Object.freeze([
  { id: "r-https", name: "harness", projectId: "p1", remoteUrl: "https://github.com/acme/harness.git" },
  { id: "r-ssh", name: "infra", projectId: "p1", remoteUrl: "git@github.com:acme/infra.git" },
  { id: "r-gitlab", name: "mirror", projectId: "p1", remoteUrl: "https://gitlab.example.com/acme/mirror.git" },
  { id: "r-local", name: "scratch", projectId: "p1", remoteUrl: null },
  { id: "r-other", name: "tui", projectId: "p2", remoteUrl: "https://github.com/acme/tui.git" },
]);

/** n minutes after C0. */
export const at = (m) => new Date(C0.getTime() + m * 60_000);

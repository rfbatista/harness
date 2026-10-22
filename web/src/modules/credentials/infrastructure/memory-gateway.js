// CredentialGateway in memory: the fake for component tests and the data
// source for web/dev pages. It obeys the same contract as the real gateway
// (../testing/gateway-contract.js): the server's validation, resolution and
// locked-store rules, and its order (a change is committed, then streamed,
// then answered). Like the server it keeps secrets only to check them and
// never hands one back.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { DEFAULT_USERNAME, applyChange, automaticFor, identityProblem, credentialFor, normalizeHost, remoteOf, sorted, takes } from "../domain/credential.js";
import { toPageSeed, toRepositoriesSeed } from "./dto.js";

/** The passphrase that opens the fake's encrypted keys. */
export const PASSPHRASE = "correct horse";

const fail = (code, message, status, details) => {
  throw new StructuredError(code, `${code}: ${message}`, status, details);
};

/**
 * @typedef {object} FakeRepository
 * @property {string} id
 * @property {string} name
 * @property {string} projectId
 * @property {string|null} remoteUrl
 * @property {string|null} [credentialId]  its pin
 */

/**
 * @param {{
 *   credentials?: import("../domain/credential.js").Credential[],
 *   identity?: import("../domain/credential.js").Identity|null,
 *   repositories?: FakeRepository[],
 *   locked?: boolean,
 *   now?: () => Date,
 *   beforeAnswer?: () => Promise<void>,  awaited after a change is streamed, before it is answered
 *   refuse?: (credential: import("../domain/credential.js").Credential) => string,  a host's refusal for test; "" signs in
 * }} [world]
 */
export function memoryCredentials({ credentials = [], identity = null, repositories = [], locked = false, now = () => new Date(), beforeAnswer = async () => {}, refuse = () => "" } = {}) {
  let list = sorted(credentials);
  let who = identity;
  let repos = repositories.map((r) => ({ credentialId: null, ...r }));
  let isLocked = locked;
  const followers = new Set();
  let nextId = 1;

  function emit(change) {
    for (const f of followers) f.onChange(change);
  }
  function commit(credential) {
    list = applyChange(list, { kind: "upsert", credential });
    emit({ kind: "upsert", credential });
    return credential;
  }
  const find = (id) => list.find((c) => c.id === id) ?? null;
  const mustFind = (id) => find(id) ?? fail(Codes.GIT_CREDENTIAL_NOT_FOUND, `git credential ${id} not found`, 404);
  const mustFindRepo = (id) => repos.find((r) => r.id === id) ?? fail(Codes.REPOSITORY_NOT_FOUND, `repository ${id} not found`, 404);
  const nameTaken = (name, except = "") => list.some((c) => c.id !== except && c.name.toLowerCase() === name.toLowerCase());
  const mustBeOpen = () => isLocked && fail(Codes.GIT_CREDENTIAL_STORE_LOCKED, "the server has no key to read stored secrets", 503);

  function checkName(raw, except) {
    const name = String(raw ?? "").trim();
    if (!name) fail(Codes.INVALID_INPUT, "name is required", 400);
    if (nameTaken(name, except)) fail(Codes.GIT_CREDENTIAL_NAME_TAKEN, `a credential named ${name} exists`, 409);
    return name;
  }
  function checkHost(raw) {
    const host = normalizeHost(raw);
    if (!host) fail(Codes.INVALID_INPUT, "host is required", 400);
    return host;
  }
  /** A private key's public half, as the fake makes it up; refuses what does not parse. */
  function openKey(privateKey, passphrase) {
    if (!/^-----BEGIN [A-Z ]*PRIVATE KEY-----/.test(String(privateKey).trim())) fail(Codes.GIT_CREDENTIAL_INVALID_KEY, "the private key does not parse", 400);
    if (privateKey.includes("ENCRYPTED") && passphrase !== PASSPHRASE) fail(Codes.GIT_CREDENTIAL_INVALID_KEY, "wrong passphrase", 400);
    const id = nextId++;
    return { publicKey: `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAImported${id} harness`, fingerprint: `SHA256:imported${id}` };
  }
  const tokenFingerprint = (token) => `…${token.slice(-4)}`;

  /** What a push from repo would use now: its pin, else the default (or only) credential for its origin. */
  const resolve = (repo) => (repo.credentialId ? find(repo.credentialId) : automaticFor(list, repo.remoteUrl));
  /** The https token GitHub API calls on repo would use: its push credential when that is a token of the host, else the host's default or only one. */
  const pullRequestsFor = (repo, resolved) => {
    const host = remoteOf(repo.remoteUrl)?.host ?? "github.com";
    return resolved?.kind === "https_token" && resolved.host === host ? resolved : credentialFor(list, host, "https_token");
  };
  const repositoryCredential = (repo) => {
    const resolved = resolve(repo);
    return Object.freeze({ repositoryId: repo.id, credentialId: repo.credentialId ?? null, resolved, pullRequests: pullRequestsFor(repo, resolved), remoteUrl: repo.remoteUrl ?? null });
  };

  /** @type {import("../domain/ports.js").CredentialGateway} */
  const gateway = {
    async list() {
      return { credentials: list, locked: isLocked };
    },

    async create(input) {
      const name = checkName(input.name);
      const host = checkHost(input.host);
      const kind = input.kind;
      if (kind !== "https_token" && kind !== "ssh_key") fail(Codes.INVALID_INPUT, "kind is https_token or ssh_key", 400);
      let fingerprint;
      let publicKey = null;
      if (kind === "https_token") {
        if (!input.token) fail(Codes.INVALID_INPUT, "token is required", 400);
        mustBeOpen();
        fingerprint = tokenFingerprint(input.token);
      } else if (input.generate) {
        mustBeOpen();
        const id = nextId++;
        publicKey = `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAGenerated${id} harness`;
        fingerprint = `SHA256:generated${id}`;
      } else {
        if (!input.privateKey) fail(Codes.INVALID_INPUT, "private_key or generate is required", 400);
        mustBeOpen();
        ({ publicKey, fingerprint } = openKey(input.privateKey, input.passphrase));
      }
      const at = now();
      const credential = Object.freeze({
        id: `gc-${nextId++}`,
        name,
        kind,
        host,
        username: input.username || DEFAULT_USERNAME[kind],
        fingerprint,
        publicKey,
        isDefault: input.isDefault === true,
        createdAt: at,
        updatedAt: at,
        lastUsedAt: null,
      });
      commit(credential);
      await beforeAnswer();
      return credential;
    },

    async update(input) {
      const old = mustFind(input.credentialId);
      const next = { ...old };
      if (input.name !== undefined) next.name = checkName(input.name, old.id);
      if (input.host !== undefined) {
        next.host = checkHost(input.host);
        const pinned = repos.filter((r) => r.credentialId === old.id);
        if (next.host !== old.host && pinned.length > 0) {
          fail(Codes.GIT_CREDENTIAL_HOST_MISMATCH, "repositories are pinned to this credential", 400, { repositories: pinned.map((r) => ({ id: r.id, name: r.name })) });
        }
      }
      if (input.username !== undefined) next.username = input.username || DEFAULT_USERNAME[old.kind];
      if (old.kind === "https_token" && input.token) {
        mustBeOpen();
        next.fingerprint = tokenFingerprint(input.token);
      }
      if (old.kind === "ssh_key" && input.privateKey) {
        mustBeOpen();
        Object.assign(next, openKey(input.privateKey, input.passphrase));
      }
      if (input.isDefault !== undefined) next.isDefault = input.isDefault === true;
      next.updatedAt = now();
      const credential = Object.freeze(next);
      commit(credential);
      await beforeAnswer();
      return credential;
    },

    async remove(credentialId) {
      mustFind(credentialId);
      list = list.filter((c) => c.id !== credentialId);
      repos = repos.map((r) => (r.credentialId === credentialId ? { ...r, credentialId: null } : r));
      emit({ kind: "deleted", id: credentialId });
      await beforeAnswer();
    },

    async test(credentialId, repositoryId) {
      const credential = mustFind(credentialId);
      const repo = repositoryId ? mustFindRepo(repositoryId) : null;
      mustBeOpen();
      let checkedUrl;
      let refusal = refuse(credential);
      if (repo) {
        checkedUrl = repo.remoteUrl ?? "";
      } else if (credential.kind === "ssh_key") {
        checkedUrl = `ssh://${credential.username}@${credential.host}`;
      } else if (credential.host === "github.com") {
        checkedUrl = "https://api.github.com/user";
      } else {
        checkedUrl = `https://${credential.host}`;
        refusal ||= "pick a repository to test this token against";
      }
      if (refusal) return Object.freeze({ ok: false, message: refusal, checkedUrl });
      commit(Object.freeze({ ...credential, lastUsedAt: now() }));
      await beforeAnswer();
      return Object.freeze({ ok: true, message: `Signed in to ${credential.host} as ${credential.username}.`, checkedUrl });
    },

    async getIdentity() {
      return who;
    },

    async updateIdentity({ name, email }) {
      const problem = identityProblem({ name, email });
      if (problem) fail(Codes.INVALID_INPUT, problem, 400);
      who = Object.freeze({ name: name.trim(), email: email.trim() });
      emit({ kind: "identity", identity: who });
      await beforeAnswer();
      return who;
    },

    async getRepositoryCredential(repositoryId) {
      return repositoryCredential(mustFindRepo(repositoryId));
    },

    async listRepositoryCredentials(projectId) {
      const mine = repos.filter((r) => r.projectId === projectId);
      if (mine.length === 0) fail(Codes.PROJECT_NOT_FOUND, `project ${projectId} not found`, 404);
      return mine.map(repositoryCredential);
    },

    async setRepositoryCredential(repositoryId, credentialId) {
      const repo = mustFindRepo(repositoryId);
      if (credentialId) {
        const credential = mustFind(credentialId);
        const remote = remoteOf(repo.remoteUrl);
        if (remote && credential.host !== remote.host) fail(Codes.GIT_CREDENTIAL_HOST_MISMATCH, `${credential.name} is for ${credential.host}, origin is on ${remote.host}`, 400);
        if (remote && !takes(remote, credential.kind)) fail(Codes.GIT_CREDENTIAL_KIND_MISMATCH, `origin takes a ${remote.kind}`, 400);
      }
      repos = repos.map((r) => (r.id === repositoryId ? { ...r, credentialId: credentialId || null } : r));
      return repositoryCredential(mustFindRepo(repositoryId));
    },

    decodePageSeed: toPageSeed,
    decodeRepositoriesSeed: toRepositoriesSeed,

    follow(onChange, onStatus = () => {}) {
      const follower = { onChange, onStatus };
      followers.add(follower);
      queueMicrotask(() => followers.has(follower) && onStatus(FeedStatus.LIVE));
      return () => followers.delete(follower);
    },
  };

  return {
    gateway,
    /** The server loses (or regains) its encryption key. */
    lock(on = true) {
      isLocked = on;
    },
    /** A change made in another browser, or by the server itself. */
    upsert: (credential) => commit(Object.freeze(credential)),
    /** The stream drops (PAUSED) or comes back having missed changes (RESYNCED). */
    reportStatus(status) {
      for (const f of followers) f.onStatus(status);
    },
    find,
    repository: (id) => repos.find((r) => r.id === id) ?? null,
  };
}

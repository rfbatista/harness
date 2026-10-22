// The wire format of git credentials (the Web UI contract "git credentials",
// rev 3) and its mapping to the domain. The server never sends a secret, so
// there is none to drop here; a field this does not know is ignored.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { KINDS } from "../domain/credential.js";

/** @returns {import("../domain/credential.js").Credential} */
export function toCredential(dto) {
  if (!dto || typeof dto.id !== "string" || dto.id === "") bad("credential without an id");
  const at = `credential ${dto.id}`;
  if (!KINDS.includes(dto.kind)) bad(`${at} has unknown kind "${dto.kind}"`);
  if (typeof dto.host !== "string" || dto.host === "") bad(`${at} has no host`);
  if (typeof dto.name !== "string" || dto.name === "") bad(`${at} has no name`);
  const createdAt = date(dto.created_at, `${at}: created_at`);
  const updatedAt = date(dto.updated_at, `${at}: updated_at`);
  if (!createdAt || !updatedAt) bad(`${at} has no created_at or updated_at`);
  return Object.freeze({
    id: dto.id,
    name: dto.name,
    kind: dto.kind,
    host: dto.host,
    username: dto.username ?? "",
    fingerprint: dto.fingerprint ?? "",
    publicKey: typeof dto.public_key === "string" && dto.public_key !== "" ? dto.public_key : null,
    isDefault: dto.is_default === true,
    createdAt,
    updatedAt,
    lastUsedAt: date(dto.last_used_at, `${at}: last_used_at`),
  });
}

/** {"name", "email"} or null. */
export function toIdentity(dto) {
  if (dto === null || dto === undefined) return null;
  if (typeof dto.name !== "string" || typeof dto.email !== "string") bad("identity without a name or email");
  return Object.freeze({ name: dto.name, email: dto.email });
}

/** {binding: {repository_id, credential_id}, resolved, pull_requests, remote_url} */
export function toRepositoryCredential(dto) {
  const id = dto?.binding?.repository_id;
  if (typeof id !== "string" || id === "") bad("repository credential without a repository_id");
  const pin = dto.binding.credential_id;
  return Object.freeze({
    repositoryId: id,
    credentialId: typeof pin === "string" && pin !== "" ? pin : null,
    resolved: dto.resolved ? toCredential(dto.resolved) : null,
    pullRequests: dto.pull_requests ? toCredential(dto.pull_requests) : null,
    remoteUrl: typeof dto.remote_url === "string" && dto.remote_url !== "" ? dto.remote_url : null,
  });
}

/** GET /api/list_git_credentials → {credentials: [...], locked} */
export function toCredentialList(body) {
  if (!body || !Array.isArray(body.credentials)) bad("expected {credentials: [...], locked}");
  return { credentials: body.credentials.map(toCredential), locked: body.locked === true };
}

/** POST /api/test_git_credential → {ok, message, checked_url} */
export function toTestResult(body) {
  if (!body || typeof body.ok !== "boolean") bad("expected {ok, message, checked_url}");
  return Object.freeze({ ok: body.ok, message: body.message ?? "", checkedUrl: body.checked_url ?? "" });
}

/** GET /api/list_repository_credentials → {repositories: [...]} */
export function toRepositoryCredentials(body) {
  if (!body || !Array.isArray(body.repositories)) bad("expected {repositories: [...]}");
  return body.repositories.map(toRepositoryCredential);
}

/** The Git credentials page's seed: {"credentials": [...], "locked", "identity": {...}|null} */
export function toPageSeed(body) {
  return { ...toCredentialList(body), identity: toIdentity(body.identity) };
}

/** The Repositories page's seed: {"project_id", "credentials": [...], "locked", "repositories": [...]} */
export function toRepositoriesSeed(body) {
  if (typeof body?.project_id !== "string" || body.project_id === "") bad("expected a project_id");
  return { projectId: body.project_id, ...toCredentialList(body), repositories: toRepositoryCredentials(body) };
}

/**
 * One /api/project_events message, as a credentials change. null for a
 * message about something else (projects share the feed).
 * @returns {import("../domain/credential.js").Change | null}
 */
export function toChange(dto) {
  if (dto?.git_identity) return { kind: "identity", identity: toIdentity(dto.git_identity) };
  if (!dto?.git_credential) return null;
  if (dto.deleted === true) {
    if (typeof dto.git_credential.id !== "string" || dto.git_credential.id === "") bad("deleted credential without an id");
    return { kind: "deleted", id: dto.git_credential.id };
  }
  return { kind: "upsert", credential: toCredential(dto.git_credential) };
}

function date(value, what) {
  if (value === undefined || value === null || value === "") return null;
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) bad(`${what} is not a time`);
  return d;
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected git credential: ${detail}.`);
}

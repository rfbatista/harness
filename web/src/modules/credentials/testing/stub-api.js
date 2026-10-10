// A stand-in for the harness server's git credential routes, backed by the
// memory gateway: a fetch and an EventSource that speak the Web UI contract's
// wire format. The real credentialsGateway runs the contract suite against
// it. It records every request body so tests can check what was sent.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryCredentials } from "../infrastructure/memory-gateway.js";
import { credentialDTO, repositoryCredentialDTO } from "./fixtures.js";

const STATUS = {
  INVALID_INPUT: 400,
  GIT_CREDENTIAL_NOT_FOUND: 404,
  REPOSITORY_NOT_FOUND: 404,
  PROJECT_NOT_FOUND: 404,
  GIT_CREDENTIAL_NAME_TAKEN: 409,
  GIT_CREDENTIAL_INVALID_KEY: 400,
  GIT_CREDENTIAL_HOST_MISMATCH: 400,
  GIT_CREDENTIAL_KIND_MISMATCH: 400,
  GIT_CREDENTIAL_STORE_LOCKED: 503,
};

export function stubCredentialsApi(world) {
  const memory = memoryCredentials(world);
  const { gateway } = memory;
  const sources = new Set();
  /** Every request: [method, path, body]. */
  const requests = [];

  const fail = (err) => jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err), details: err.details });
  const identityDTO = (i) => (i ? { name: i.name, email: i.email } : null);

  const routes = {
    "GET /api/list_git_credentials": async () => ({ credentials: (await gateway.list()).map(credentialDTO) }),
    "POST /api/create_git_credential": async (b) => ({
      credential: credentialDTO(
        await gateway.create({ name: b.name, kind: b.kind, host: b.host, username: b.username, token: b.token, privateKey: b.private_key, passphrase: b.passphrase, generate: b.generate, isDefault: b.is_default }),
      ),
    }),
    "POST /api/update_git_credential": async (b) => ({
      credential: credentialDTO(
        await gateway.update({ credentialId: b.credential_id, name: b.name, host: b.host, username: b.username, token: b.token, privateKey: b.private_key, passphrase: b.passphrase, isDefault: b.is_default }),
      ),
    }),
    "POST /api/delete_git_credential": async (b) => (await gateway.remove(b.credential_id), undefined),
    "POST /api/test_git_credential": async (b) => {
      const r = await gateway.test(b.credential_id, b.repository_id);
      return { ok: r.ok, message: r.message, checked_url: r.checkedUrl };
    },
    "GET /api/get_git_identity": async () => ({ identity: identityDTO(await gateway.getIdentity()) }),
    "POST /api/update_git_identity": async (b) => ({ identity: identityDTO(await gateway.updateIdentity({ name: b.name, email: b.email })) }),
    "GET /api/get_repository_credential": async (_, q) => repositoryCredentialDTO(await gateway.getRepositoryCredential(q.get("repository_id") ?? "")),
    "GET /api/list_repository_credentials": async (_, q) => ({ repositories: (await gateway.listRepositoryCredentials(q.get("project_id") ?? "")).map(repositoryCredentialDTO) }),
    "POST /api/set_repository_credential": async (b) => repositoryCredentialDTO(await gateway.setRepositoryCredential(b.repository_id, b.credential_id)),
  };

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    requests.push([method, url.pathname, body]);
    const route = routes[`${method} ${url.pathname}`];
    if (!route) return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    try {
      const answer = await route(body, url.searchParams);
      return answer === undefined ? new Response(null, { status: 204 }) : jsonResponse(200, answer);
    } catch (err) {
      return fail(err);
    }
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      if (url.pathname !== "/api/project_events") throw new Error(`no stream at ${url.pathname}`);
      sources.add(this);
      this.unfollow = gateway.follow((change) => {
        if (change.kind === "upsert") this.send({ git_credential: credentialDTO(change.credential) });
        if (change.kind === "deleted") this.send({ git_credential: { id: change.id }, deleted: true });
        if (change.kind === "identity") this.send({ git_identity: identityDTO(change.identity) });
      });
      queueMicrotask(() => this.onopen?.({}));
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      this.unfollow();
      sources.delete(this);
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    requests,
    /** Sends a raw message on every open stream (projects share it). */
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}

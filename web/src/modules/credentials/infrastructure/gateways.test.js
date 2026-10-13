// Both CredentialGateway implementations run the same contract.

import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeFetch, flush, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { credentialGatewayContract } from "../testing/gateway-contract.js";
import { SECRET_TOKEN, credentialDTO, makeCredential } from "../testing/fixtures.js";
import { stubCredentialsApi } from "../testing/stub-api.js";
import { credentialsGateway } from "./credentials-gateway.js";
import { toChange, toCredential, toRepositoryCredential, toTestResult } from "./dto.js";
import { memoryCredentials } from "./memory-gateway.js";

file("credentials/infrastructure/gateways");

credentialGatewayContract("memory", (world) => {
  const memory = memoryCredentials(world);
  return { gateway: memory.gateway, lock: memory.lock };
});

function http(world) {
  const stub = stubCredentialsApi(world);
  const gateway = credentialsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

credentialGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, lock: stub.memory.lock };
});

test("http · the paths, queries and bodies are the contract's; empty secrets and absent fields are not sent", async () => {
  const { fetch, calls } = fakeFetch((url) => {
    if (url.includes("list_repository_credentials")) return jsonResponse(200, { repositories: [] });
    if (url.includes("repository_credential")) return jsonResponse(200, { binding: { repository_id: "r1", credential_id: null }, resolved: null, remote_url: null });
    if (url.includes("test_git")) return jsonResponse(200, { ok: true, message: "", checked_url: "" });
    if (url.includes("identity")) return jsonResponse(200, { identity: null });
    if (url.includes("delete")) return new Response(null, { status: 204 });
    if (url.includes("list_git")) return jsonResponse(200, { credentials: [] });
    return jsonResponse(200, { credential: credentialDTO(makeCredential()) });
  });
  const gateway = credentialsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: class {} }));
  await gateway.list();
  await gateway.create({ name: "ci", kind: "https_token", host: "github.com", username: "", token: "t", isDefault: true });
  await gateway.create({ name: "k", kind: "ssh_key", host: "github.com", generate: true });
  await gateway.update({ credentialId: "gc-1", name: "n", token: "", privateKey: "", passphrase: "" });
  await gateway.update({ credentialId: "gc-1", isDefault: false });
  await gateway.remove("gc-1");
  await gateway.test("gc-1");
  await gateway.test("gc-1", "r1");
  await gateway.getIdentity();
  await gateway.updateIdentity({ name: "Ada", email: "a@b" });
  await gateway.getRepositoryCredential("r 1");
  await gateway.listRepositoryCredentials("p1");
  await gateway.setRepositoryCredential("r1", null);
  await gateway.setRepositoryCredential("r1", "gc-1");
  assert.deepEqual(
    calls.map((c) => [c.init.method, c.url, c.init.body ?? null]),
    [
      ["GET", "/api/list_git_credentials", null],
      ["POST", "/api/create_git_credential", '{"name":"ci","kind":"https_token","host":"github.com","token":"t","is_default":true}'],
      ["POST", "/api/create_git_credential", '{"name":"k","kind":"ssh_key","host":"github.com","generate":true}'],
      ["POST", "/api/update_git_credential", '{"credential_id":"gc-1","name":"n"}'],
      ["POST", "/api/update_git_credential", '{"credential_id":"gc-1","is_default":false}'],
      ["POST", "/api/delete_git_credential", '{"credential_id":"gc-1"}'],
      ["POST", "/api/test_git_credential", '{"credential_id":"gc-1"}'],
      ["POST", "/api/test_git_credential", '{"credential_id":"gc-1","repository_id":"r1"}'],
      ["GET", "/api/get_git_identity", null],
      ["POST", "/api/update_git_identity", '{"name":"Ada","email":"a@b"}'],
      ["GET", "/api/get_repository_credential?repository_id=r+1", null],
      ["GET", "/api/list_repository_credentials?project_id=p1", null],
      ["POST", "/api/set_repository_credential", '{"repository_id":"r1","credential_id":null}'],
      ["POST", "/api/set_repository_credential", '{"repository_id":"r1","credential_id":"gc-1"}'],
    ],
  );
});

test("http · a refusal keeps its code and details", async () => {
  const { gateway } = http({ credentials: [makeCredential()], repositories: [{ id: "r1", name: "harness", projectId: "p1", remoteUrl: "https://github.com/a/b.git", credentialId: "gc-work" }] });
  const err = await assert.rejects(gateway.update({ credentialId: "gc-work", host: "gitlab.com" }), Codes.GIT_CREDENTIAL_HOST_MISMATCH);
  assert.deepEqual([err.status, err.details?.repositories], [400, [{ id: "r1", name: "harness" }]]);
  assert.ok(err.message.startsWith("GIT_CREDENTIAL_HOST_MISMATCH"));
});

test("http · project messages and malformed ones on the shared feed are skipped; the stream stays open", async () => {
  const { gateway, stub } = http({});
  const changes = [];
  const close = gateway.follow((c) => changes.push(c));
  await flush();
  stub.pushRaw({ project: { id: "p1", name: "harness", root_dir: "/src" } });
  stub.pushRaw({ git_credential: { id: "" } });
  stub.pushRaw({ git_credential: { id: "x", kind: "password" } });
  stub.pushRaw("not json");
  await stub.memory.gateway.updateIdentity({ name: "After", email: "a@b" });
  await flush();
  assert.deepEqual(changes.map((c) => c.kind), ["identity"]);
  close();
});

test("http · no request the page makes carries a secret back in an answer", async () => {
  const { gateway, stub } = http({});
  await gateway.create({ name: "ci", kind: "https_token", host: "github.com", token: SECRET_TOKEN });
  const answers = await Promise.all([stub.fetch("/api/list_git_credentials").then((r) => r.text())]);
  assert.ok(!answers.join("").includes(SECRET_TOKEN));
  assert.deepEqual(stub.requests.filter((r) => JSON.stringify(r).includes(SECRET_TOKEN)).map((r) => r[1]), ["/api/create_git_credential"], "the token was only ever sent, once");
});

test("a credential without its optional fields reads them as empty; it is frozen", () => {
  const c = toCredential({ id: "gc-1", name: "n", kind: "ssh_key", host: "github.com", created_at: "2026-10-10T18:00:00Z", updated_at: "2026-10-10T18:00:00Z" });
  assert.deepEqual([c.username, c.fingerprint, c.publicKey, c.isDefault, c.lastUsedAt], ["", "", null, false, null]);
  assert.ok(Object.isFrozen(c));
});

test("a malformed credential, binding, test result or change is BAD_RESPONSE", () => {
  const dto = credentialDTO(makeCredential());
  assert.throws(() => toCredential({ ...dto, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toCredential({ ...dto, kind: "password" }), Codes.BAD_RESPONSE);
  assert.throws(() => toCredential({ ...dto, host: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toCredential({ ...dto, created_at: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toCredential({ ...dto, last_used_at: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toRepositoryCredential({ binding: {}, resolved: null }), Codes.BAD_RESPONSE);
  assert.throws(() => toTestResult({ message: "x" }), Codes.BAD_RESPONSE);
  assert.throws(() => toChange({ git_credential: { id: "" }, deleted: true }), Codes.BAD_RESPONSE);
  assert.equal(toChange({ project: { id: "p1" } }), null);
  assert.deepEqual(toChange({ git_credential: { id: "gc-1" }, deleted: true }), { kind: "deleted", id: "gc-1" });
  assert.equal(toRepositoryCredential({ binding: { repository_id: "r1", credential_id: "" }, resolved: null, remote_url: "" }).remoteUrl, null);
});

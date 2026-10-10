// Both CodeGateway implementations run the same contract.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { codeGatewayContract } from "../testing/gateway-contract.js";
import { makeWorld } from "../testing/fixtures.js";
import { stubCodeApi } from "../testing/stub-api.js";
import { codeGateway } from "./code-gateway.js";
import { toCodeChanged, toDiff, toSeed, toTree } from "./dto.js";
import { memoryCode } from "./memory-gateway.js";

file("code/infrastructure/gateways");

codeGatewayContract("memory", (world) => {
  const memory = memoryCode(world);
  return { gateway: memory.gateway, memory, change: memory.change };
});

function http(world) {
  const stub = stubCodeApi(world);
  const gateway = codeGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

codeGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, memory: stub.memory, change: stub.change };
});

test("http · reads send the contract's query: one source, ref only with a repository", async () => {
  const { gateway, stub } = http(makeWorld());
  await gateway.tree({ repositoryId: "r1", ref: "v0.9.0" }, "");
  await gateway.file({ workspaceId: "ws1" }, "README.md");
  await gateway.diff({ workspaceId: "ws1", scope: "working" });
  await gateway.diff({ repositoryId: "r1", commit: "abcd" }).catch(() => {});
  assert.deepEqual(stub.requested, [
    "/api/code/tree?repository_id=r1&ref=v0.9.0",
    "/api/code/file?workspace_id=ws1&path=README.md",
    "/api/code/diff?workspace_id=ws1&scope=working",
    "/api/code/diff?repository_id=r1&commit=abcd",
  ]);
});

test("http · other feed messages and a malformed code_changed are dropped; the stream stays open", async () => {
  const { gateway, stub } = http(makeWorld());
  const seen = [];
  const stop = gateway.follow("p1", (c) => seen.push(c), () => {});
  await flush();
  stub.send({ session: { id: "s1" } });
  stub.send({ code_changed: { head: "x" } });
  stub.send({ code_changed: { workspace_id: "ws1", head: "h" } });
  await flush();
  assert.deepEqual(seen, [{ workspaceId: "ws1", head: "h" }]);
  stop();
});

test("dto · unknown kinds and statuses are BAD_RESPONSE, not silently drawn", () => {
  const src = { kind: "ref", repository_id: "r1", workspace_id: null, ref: "main", commit: "a", gone: false };
  const err = assert.throws(() => toTree({ source: src, path: "", entries: [{ path: "x", kind: "socket" }] }), Codes.BAD_RESPONSE);
  assert.ok(err instanceof StructuredError);
  assert.throws(() => toDiff({ source: src, scope: "all", files: [{ path: "x", status: "exploded" }] }), Codes.BAD_RESPONSE);
  assert.throws(() => toDiff({ source: src, scope: "sideways", files: [] }), Codes.BAD_RESPONSE);
  assert.equal(toCodeChanged({ session: {} }), null);
});

test("dto · binary counts stay -1 and a withheld file has no hunks", () => {
  const src = { kind: "workspace", repository_id: "r1", workspace_id: "ws1", ref: "b", commit: "c", gone: true };
  const d = toDiff({ source: src, base: "b", scope: "branch", files: [{ path: "a.png", status: "added", binary: true, added: -1, deleted: -1, hunks: null, withheld: "binary", language: "", zones: null }], added: 0, deleted: 0, truncated: false });
  const f = d.files[0];
  assert.deepEqual([f.added, f.deleted, f.hunks, f.withheld, f.language, f.zones, d.source.gone], [-1, -1, null, "binary", "plaintext", [], true]);
  assert.ok(Object.isFrozen(f));
});

test("dto · a seed carries the route, the source and what was painted", () => {
  const memory = memoryCode(makeWorld());
  const seed = toSeed({
    project_id: "p1",
    route: { kind: "session", task_id: "t1", session_id: "s1", view: "files", path: "README.md" },
    source: { workspace_id: "ws1" },
    repository_name: "harness",
    branch: "agent/visualize-code",
    base_ref: "main",
    live: true,
    trees: [memory.wire.tree({ workspaceId: "ws1" }, "")],
    file: memory.wire.file({ workspaceId: "ws1" }, "README.md"),
    diff: null,
    map: null,
    error: null,
  });
  assert.deepEqual([seed.route.kind, seed.route.projectId, seed.route.view, seed.route.path, seed.source.workspaceId, seed.live, seed.baseRef], ["session", "p1", "files", "README.md", "ws1", true, "main"]);
  assert.equal(seed.trees[0].entries.length > 0, true);
  assert.equal(seed.file.path, "README.md");
  assert.deepEqual(toSeed({ error: { code: "UNAVAILABLE", message: "off" } }).error, { code: "UNAVAILABLE", message: "off" });
});

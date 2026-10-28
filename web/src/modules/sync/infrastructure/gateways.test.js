import { Codes } from "../../../shared/domain/errors.js";
import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { syncGatewayContract } from "../testing/gateway-contract.js";
import { HEAD, makeState } from "../testing/fixtures.js";
import { stubSyncApi } from "../testing/stub-api.js";
import { toGitState, toGitStateDTO, toGitStateEvent, toSeed, toStates } from "./dto.js";
import { memorySync } from "./memory-gateway.js";
import { syncGateway } from "./sync-gateway.js";

file("sync/infrastructure/gateways");

syncGatewayContract("memory", (world) => {
  const memory = memorySync(world);
  return { gateway: memory.gateway, world: memory };
});

function http(world) {
  const stub = stubSyncApi(world);
  const gateway = syncGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

syncGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, world: stub.memory };
});

test("http · the reads are GETs and the actions POSTs with the contract's bodies", async () => {
  const stub = stubSyncApi({ workspaces: [{ state: makeState({ upstream: "ahead" }) }] });
  const calls = [];
  const fetch = (input, init = {}) => (calls.push([init.method ?? "GET", input, init.body]), stub.fetch(input, init));
  const gateway = syncGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  await gateway.get("ws1");
  await gateway.listForTask("t1");
  await gateway.fetchRemote("r1");
  await gateway.push({ workspaceId: "ws1", expectedHead: HEAD });
  assert.deepEqual(calls, [
    ["GET", "/api/worktree_git_state?workspace_id=ws1", undefined],
    ["GET", "/api/list_worktree_git_states?task_id=t1", undefined],
    ["POST", "/api/fetch_remote", JSON.stringify({ repository_id: "r1" })],
    ["POST", "/api/push_branch", JSON.stringify({ workspace_id: "ws1", expected_head: HEAD })],
  ]);
});

test("a state that cannot be read is BAD_RESPONSE", () => {
  assert.throws(() => toGitState({}), Codes.BAD_RESPONSE);
  const ok = toGitStateDTO(makeState());
  assert.throws(() => toGitState({ ...ok, upstream: { ...ok.upstream, state: "sideways" } }), Codes.BAD_RESPONSE);
  assert.throws(() => toGitState({ ...ok, read_at: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toGitState({ ...ok, fetched_at: "soon" }), Codes.BAD_RESPONSE);
  assert.throws(() => toStates({}), Codes.BAD_RESPONSE);
});

test("the wire shape round-trips: nulls stay null, counts are numbers, dates are dates", () => {
  const state = makeState({ upstream: "none", fetchedAt: null, branch: "", head: "" });
  assert.deepEqual(toGitState(toGitStateDTO(state)), state);
  const full = makeState({ upstream: "diverged" });
  assert.deepEqual(toGitState(toGitStateDTO(full)), full);
});

test("a feed message carries a state under git_state; any other key is ignored", () => {
  assert.equal(toGitStateEvent({ code_changed: {} }), null);
  assert.equal(toGitStateEvent({ git_state: toGitStateDTO(makeState()) }).workspaceId, "ws1");
  assert.throws(() => toGitStateEvent({ git_state: { workspace_id: "ws1" } }), Codes.BAD_RESPONSE);
});

test("a page's seed carries the project and its states", () => {
  const seed = toSeed({ project_id: "p1", git_states: [toGitStateDTO(makeState()), toGitStateDTO(makeState({ workspaceId: "ws2" }))] });
  assert.deepEqual([seed.projectId, seed.states.map((s) => s.workspaceId)], ["p1", ["ws1", "ws2"]]);
  assert.throws(() => toSeed(null), Codes.BAD_RESPONSE);
});

test("http · a malformed git_state on the feed is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({ workspaces: [{ state: makeState() }] });
  const seen = [];
  gateway.follow("p1", (s) => seen.push(s.workspaceId), () => {});
  await flush();
  stub.pushRaw({ git_state: { workspace_id: "ws1", upstream: { state: "sideways" } } });
  stub.pushRaw("not json");
  stub.pushRaw({ git_state: toGitStateDTO(makeState({ workspaceId: "ws9" })) });
  await flush();
  assert.deepEqual(seen, ["ws9"]);
});

test("http · a refused push hands the module the state after a re-read as a GitState", async () => {
  const { gateway } = http({ workspaces: [{ state: makeState({ upstream: "diverged" }) }] });
  const err = await assert.rejects(gateway.push({ workspaceId: "ws1", expectedHead: HEAD }), Codes.PUSH_REJECTED);
  assert.ok(err.details.state.readAt instanceof Date);
  assert.equal(err.details.state.upstream.state, "diverged");
});

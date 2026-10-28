// The SyncGateway contract (../domain/ports.js), run by every implementation.
// A fake that passes it behaves like the real gateway as far as the chip, the
// panel and the task page's popover can tell.

import { Codes } from "../../../shared/domain/errors.js";
import { FeedStatus } from "../../../shared/domain/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, test } from "../../../shared/testing/test.js";
import { HEAD, makeState, T0 } from "./fixtures.js";

/**
 * @typedef {object} Subject
 * @property {import("../domain/ports.js").SyncGateway} gateway
 * @property {ReturnType<typeof import("../infrastructure/memory-gateway.js").memorySync>} world  plays the server's side
 */

/** @param {string} name @param {(world: object) => Subject} makeSubject */
export function syncGatewayContract(name, makeSubject) {
  const contract = (title, fn) => test(`${name} · ${title}`, fn);
  const ws = (upstream = "ahead", extra = {}, state = {}) => ({ state: makeState({ upstream, ...state }), ...extra });
  const now = () => new Date(T0.getTime() + 60000);

  contract("get: the state as the server read it, with its dates as dates", async () => {
    const { gateway } = makeSubject({ workspaces: [ws("ahead")], now });
    const s = await gateway.get("ws1");
    assert.deepEqual([s.workspaceId, s.branch, s.head, s.upstream.state, s.upstream.ref, s.upstream.ahead, s.upstream.behind], ["ws1", "agent/sync-1234", HEAD, "ahead", "origin/agent/sync-1234", 3, 0]);
    assert.deepEqual([s.base.ref, s.base.ahead, s.base.behind, s.uncommitted.tracked, s.uncommitted.untracked, s.uncommitted.staged], ["main", 3, 8, 3, 2, 1]);
    assert.ok(s.fetchedAt instanceof Date && s.readAt instanceof Date);
  });

  contract("get: no upstream has null counts, a never fetched one has no fetch time", async () => {
    const { gateway } = makeSubject({ workspaces: [ws("none", {}, { fetchedAt: null })] });
    const s = await gateway.get("ws1");
    assert.deepEqual([s.upstream.state, s.upstream.ref, s.upstream.remote, s.upstream.ahead, s.fetchedAt], ["none", null, null, null, null]);
  });

  contract("get: a workspace that is not there is WORKSPACE_NOT_FOUND", async () => {
    const { gateway } = makeSubject({ workspaces: [] });
    await assert.rejects(gateway.get("ws-gone"), Codes.WORKSPACE_NOT_FOUND);
  });

  contract("listForTask answers the task's states in order, and needs a task", async () => {
    const { gateway } = makeSubject({ workspaces: [ws("ahead"), ws("none", {}, { workspaceId: "ws2" }), ws("ahead", { taskId: "t2" }, { workspaceId: "ws3" })] });
    assert.deepEqual((await gateway.listForTask("t1")).map((s) => s.workspaceId), ["ws1", "ws2"]);
    await assert.rejects(gateway.listForTask(""), Codes.INVALID_INPUT);
  });

  contract("fetchRemote answers when it fetched; the new counts follow on get and on the feed", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("in_sync")], now });
    world.remoteMoves("ws1", { ahead: 0, behind: 4 });
    const seen = [];
    gateway.follow("p1", (s) => seen.push(s), () => {});
    const { fetchedAt } = await gateway.fetchRemote("r1");
    assert.equal(fetchedAt.toISOString(), now().toISOString());
    const s = await gateway.get("ws1");
    assert.deepEqual([s.upstream.state, s.upstream.behind, s.fetchedAt.toISOString()], ["behind", 4, now().toISOString()]);
    await flush();
    assert.deepEqual(seen.map((x) => [x.workspaceId, x.upstream.state]), [["ws1", "behind"]]);
  });

  contract("fetchRemote makes a never fetched upstream readable", async () => {
    const { gateway } = makeSubject({ workspaces: [ws("unknown", {}, { fetchedAt: null })], now });
    await gateway.fetchRemote("r1");
    assert.equal((await gateway.get("ws1")).upstream.state, "in_sync");
  });

  contract("fetchRemote refusals: no repository, no origin, no credential (naming the host), a failed fetch", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("in_sync")] });
    await assert.rejects(gateway.fetchRemote("r-gone"), Codes.REPOSITORY_NOT_FOUND);
    world.missCredential("ws1", { host: "gitlab.example.com", kind: "ssh_key" });
    const err = await assert.rejects(gateway.fetchRemote("r1"), Codes.GIT_CREDENTIAL_MISSING);
    assert.deepEqual(err.details, { host: "gitlab.example.com", kind: "ssh_key" });
    world.missCredential("ws1", null);
    world.failNext(Codes.FETCH_FAILED, "Could not resolve host");
    const failed = await assert.rejects(gateway.fetchRemote("r1"), Codes.FETCH_FAILED);
    assert.ok(failed.message.includes("Could not resolve host"));
    const noRemote = makeSubject({ workspaces: [ws("none", { noRemote: true })] });
    await assert.rejects(noRemote.gateway.fetchRemote("r1"), Codes.NO_REMOTE);
  });

  contract("push publishes the branch, sets the upstream and answers the state after", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("none")], now });
    const s = await gateway.push({ workspaceId: "ws1", expectedHead: HEAD });
    assert.deepEqual([s.upstream.state, s.upstream.ref, s.upstream.remote, s.upstream.ahead, s.upstream.behind], ["in_sync", "origin/agent/sync-1234", "origin", 0, 0]);
    assert.equal((await gateway.get("ws1")).upstream.state, "in_sync");
    assert.deepEqual(world.pushed, [{ workspaceId: "ws1", expectedHead: HEAD }]);
  });

  contract("push of a head that moved is PUSH_STALE carrying the state now, and pushes nothing", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("ahead")] });
    const err = await assert.rejects(gateway.push({ workspaceId: "ws1", expectedHead: "0000000" }), Codes.PUSH_STALE);
    assert.equal(err.details.state.workspaceId, "ws1");
    assert.equal(err.details.state.head, HEAD);
    assert.equal(world.pushed.length, 0);
  });

  contract("push of a branch behind or diverged is PUSH_REJECTED carrying the state, never forced", async () => {
    for (const upstream of ["behind", "diverged"]) {
      const { gateway, world } = makeSubject({ workspaces: [ws(upstream)] });
      const err = await assert.rejects(gateway.push({ workspaceId: "ws1", expectedHead: HEAD }), Codes.PUSH_REJECTED);
      assert.equal(err.details.state.upstream.state, upstream);
      assert.equal(world.pushed.length, 0);
    }
  });

  contract("push refusals: no workspace, a gone worktree, no credential, any other failure", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("ahead"), ws("ahead", {}, { workspaceId: "ws2", gone: true })] });
    await assert.rejects(gateway.push({ workspaceId: "ws-x", expectedHead: HEAD }), Codes.WORKSPACE_NOT_FOUND);
    await assert.rejects(gateway.push({ workspaceId: "ws2", expectedHead: HEAD }), Codes.WORKSPACE_MISSING);
    world.failNext(Codes.PUSH_FAILED, "remote hung up");
    await assert.rejects(gateway.push({ workspaceId: "ws1", expectedHead: HEAD }), Codes.PUSH_FAILED);
    world.missCredential("ws1");
    const err = await assert.rejects(gateway.push({ workspaceId: "ws1", expectedHead: HEAD }), Codes.GIT_CREDENTIAL_MISSING);
    assert.deepEqual(err.details, { host: "github.com", kind: "https_token" });
  });

  contract("follow reports live, delivers the project's git states and stops when closed", async () => {
    const { gateway, world } = makeSubject({ workspaces: [ws("ahead"), ws("ahead", { projectId: "p2" }, { workspaceId: "ws-other" })] });
    const seen = [];
    const statuses = [];
    const close = gateway.follow("p1", (s) => seen.push(`${s.workspaceId}:${s.uncommitted.tracked}`), (st) => statuses.push(st));
    await flush();
    assert.ok(statuses.includes(FeedStatus.LIVE), "reports live once connected");
    world.change("ws1", { uncommitted: { tracked: 9, untracked: 0, staged: 0 } });
    world.change("ws-other", { uncommitted: { tracked: 1, untracked: 0, staged: 0 } });
    await flush();
    assert.deepEqual(seen, ["ws1:9"]);
    close();
    world.change("ws1", { uncommitted: { tracked: 10, untracked: 0, staged: 0 } });
    await flush();
    assert.equal(seen.length, 1, "nothing after close");
  });
}

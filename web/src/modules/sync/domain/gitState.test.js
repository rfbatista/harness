import { assert, file, test } from "../../../shared/testing/test.js";
import { makeState, T0, UPSTREAMS } from "../testing/fixtures.js";
import { canPush, commitsToPush, fresher, isOld, pushTarget, upstreamStateOf } from "./gitState.js";

file("sync/domain/gitState");

test("the upstream state follows the contract's table", () => {
  const at = new Date(T0);
  const s = (o) => upstreamStateOf({ ref: "origin/x", ahead: 0, behind: 0, fetchedAt: at, ...o });
  assert.deepEqual(
    [s({ ref: null }), s({ exists: false }), s({ fetchedAt: null }), s({}), s({ ahead: 2 }), s({ behind: 2 }), s({ ahead: 1, behind: 1 })],
    ["none", "gone", "unknown", "in_sync", "ahead", "behind", "diverged"],
  );
});

test("push is offered when ahead of the upstream, or with no upstream and commits, and in no other state", () => {
  const offered = Object.keys(UPSTREAMS).filter((upstream) => canPush(makeState({ upstream })));
  assert.deepEqual(offered, ["ahead", "none"]);
});

test("with no upstream push needs commits: none ahead of the base, a detached HEAD or a gone worktree offer nothing", () => {
  const none = (o) => canPush(makeState({ upstream: "none", ...o }));
  assert.equal(none({ base: { ref: "main", ahead: 0, behind: 2 } }), false, "nothing the base lacks");
  assert.equal(none({ base: { ref: "", ahead: 0, behind: 0 } }), true, "the base is gone: the head decides");
  assert.equal(none({ head: "" }), false, "before the first commit");
  assert.equal(none({ branch: "" }), false, "detached");
  assert.equal(none({ gone: true }), false, "worktree gone");
  assert.equal(canPush(makeState({ upstream: "ahead", gone: true })), false);
});

test("the commits a push publishes: ahead of the upstream, or of the base with no upstream", () => {
  assert.equal(commitsToPush(makeState({ upstream: "ahead" })), 3);
  assert.equal(commitsToPush(makeState({ upstream: "none", base: { ref: "main", ahead: 5, behind: 0 } })), 5);
  assert.equal(commitsToPush(makeState({ upstream: "none", base: { ref: "", ahead: 0, behind: 0 } })), null);
});

test("the push target is the upstream's ref, or origin/<branch> when there is none", () => {
  assert.equal(pushTarget(makeState({ upstream: "ahead" })), "origin/agent/sync-1234");
  assert.equal(pushTarget(makeState({ upstream: "none" })), "origin/agent/sync-1234");
  assert.equal(pushTarget(makeState({ upstream: "none", branch: "agent/other" })), "origin/agent/other");
});

test("the state the server read last wins; a tie goes to the incoming one", () => {
  const older = makeState({ readAt: new Date(T0.getTime() - 1000) });
  const newer = makeState({ readAt: new Date(T0.getTime() + 1000), upstream: "none" });
  assert.equal(fresher(null, older), older);
  assert.equal(fresher(newer, older), newer);
  assert.equal(fresher(older, newer), newer);
  const twin = makeState({ upstream: "behind" });
  assert.equal(fresher(makeState(), twin), twin);
});

test("a fetch is old only past an hour, and never fetched is old", () => {
  const ago = (ms) => new Date(T0.getTime() - ms);
  assert.deepEqual([isOld(ago(59 * 60000), T0), isOld(ago(60 * 60000), T0), isOld(ago(60 * 60000 + 1), T0), isOld(null, T0)], [false, false, true, true]);
});

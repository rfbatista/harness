import { FeedStatus } from "../../../shared/domain/feed.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { catalogEventPaths, refreshScheduler } from "./liveRefresh.js";

file("agents/domain/liveRefresh");

/** Timers that run only when the test says so. */
function fakeTimers() {
  const queue = new Map();
  let next = 1;
  return {
    setTimeout: (fn, ms) => {
      queue.set(next, { fn, ms });
      return next++;
    },
    clearTimeout: (id) => queue.delete(id),
    get pending() {
      return queue.size;
    },
    async fire() {
      const [id, t] = [...queue.entries()][0] ?? [];
      if (id === undefined) return false;
      queue.delete(id);
      t.fn();
      await Promise.resolve();
      return true;
    },
  };
}
const settle = () => new Promise((r) => setTimeout(r, 0));

function subject() {
  const timers = fakeTimers();
  const reads = [];
  let release = null;
  const s = refreshScheduler({
    timers,
    reload: (req) => {
      reads.push(req);
      return new Promise((r) => { release = r; });
    },
  });
  return { s, timers, reads, finish: async () => { release?.(); await settle(); } };
}

test("only the catalog's one-key event is a catalog event; others on the page feed are not", () => {
  assert.deepEqual(catalogEventPaths({ agent_catalog_changed: { paths: ["agents.json", "skills/tdd"] } }), ["agents.json", "skills/tdd"]);
  assert.deepEqual(catalogEventPaths({ agent_catalog_changed: { paths: [] } }), []);
  assert.deepEqual(catalogEventPaths({ agent_catalog_changed: {} }), []);
  assert.deepEqual(catalogEventPaths({ agent_catalog_changed: null }), []);
  for (const other of [{ project: { id: "p1" } }, { git_credential: {} }, { git_identity: {} }, null, undefined, "x", 3, [], {}]) {
    assert.equal(catalogEventPaths(other), null, JSON.stringify(other));
  }
});

test("unrelated events and statuses never read the catalog", async () => {
  const { s, timers, reads } = subject();
  s.event({ project: { id: "p1" } });
  s.event({ git_identity: {} });
  s.event(null);
  s.status(FeedStatus.CONNECTING);
  s.status(FeedStatus.LIVE);
  assert.equal(timers.pending, 0);
  assert.equal(reads.length, 0);
});

test("a burst of events is one read that names every path once", async () => {
  const { s, timers, reads } = subject();
  s.event({ agent_catalog_changed: { paths: ["agents.json"] } });
  s.event({ agent_catalog_changed: { paths: ["skills/tdd", "agents.json"] } });
  s.event({ agent_catalog_changed: { paths: [] } });
  assert.equal(timers.pending, 1, "one timer for the whole burst");
  await timers.fire();
  assert.equal(reads.length, 1);
  assert.deepEqual(reads[0].paths, ["agents.json", "skills/tdd"]);
  assert.deepEqual(reads[0].reasons, ["event"]);
});

test("an event that arrives while a read is in flight is read once more after it, not during it", async () => {
  const { s, timers, reads, finish } = subject();
  s.event({ agent_catalog_changed: { paths: ["agents.json"] } });
  await timers.fire();
  s.event({ agent_catalog_changed: { paths: ["skills/new"] } });
  s.event({ agent_catalog_changed: { paths: ["skills/other"] } });
  assert.equal(reads.length, 1);
  assert.equal(timers.pending, 0, "no second read starts beside the first");
  await finish();
  assert.equal(timers.pending, 1);
  await timers.fire();
  assert.equal(reads.length, 2);
  assert.deepEqual(reads[1].paths, ["skills/new", "skills/other"]);
});

test("a feed that dropped and came back reads once; the first connection does not", async () => {
  const { s, timers, reads } = subject();
  s.status(FeedStatus.CONNECTING);
  s.status(FeedStatus.LIVE);
  assert.equal(timers.pending, 0);
  s.status(FeedStatus.PAUSED);
  assert.equal(timers.pending, 0, "paused alone reads nothing");
  s.status(FeedStatus.RESYNCED);
  s.status(FeedStatus.LIVE);
  await timers.fire();
  assert.equal(reads.length, 1);
  assert.deepEqual(reads[0].reasons, ["resynced"]);
});

test("regaining focus reads, and a focus inside a burst joins it", async () => {
  const { s, timers, reads } = subject();
  s.focus();
  s.event({ agent_catalog_changed: { paths: ["agents.json"] } });
  s.focus();
  await timers.fire();
  assert.equal(reads.length, 1);
  assert.deepEqual(reads[0].reasons, ["focus", "event"]);
});

test("Reload from disk reads at once, without waiting for the burst", async () => {
  const { s, timers, reads } = subject();
  s.event({ agent_catalog_changed: { paths: ["agents.json"] } });
  s.now();
  assert.equal(timers.pending, 1);
  await timers.fire();
  assert.equal(reads.length, 1);
  assert.deepEqual(reads[0].reasons, ["event", "manual"]);
});

test("a read that fails does not stop the next one; a disposed scheduler reads nothing", async () => {
  const timers = fakeTimers();
  let calls = 0;
  const s = refreshScheduler({ timers, reload: async () => { calls++; throw new Error("down"); } });
  s.focus();
  await timers.fire();
  await settle();
  s.focus();
  assert.equal(timers.pending, 1);
  await timers.fire();
  await settle();
  assert.equal(calls, 2);
  s.focus();
  s.dispose();
  assert.equal(timers.pending, 0);
  s.event({ agent_catalog_changed: { paths: [] } });
  assert.equal(timers.pending, 0);
});

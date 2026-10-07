import { assert, file, test } from "../../../shared/testing/test.js";
import { activityByTask, applyRailChange, linkState } from "./activity.js";

file("tasks/domain/activity");

const s = (id, ticketId, status, pendingApprovals = 0) => ({ id, ticketId, status, pendingApprovals });

test("counts live sessions per task and marks one waiting on you", () => {
  const byTask = activityByTask([
    s("a", "t1", "running"),
    s("b", "t1", "idle"),
    s("c", "t1", "done"),
    s("d", "t2", "thinking", 1),
    s("e", "t3", "failed"),
    s("f", "", "running"),
  ]);
  assert.deepEqual(byTask, {
    t1: { live: 2, attention: true },
    t2: { live: 1, attention: true },
    t3: { live: 0, attention: false },
  });
});

test("a link's dot: waiting beats running; nothing for a quiet task", () => {
  assert.deepEqual(linkState({ live: 2, attention: true }), { state: "waiting", word: "waiting on you" });
  assert.deepEqual(linkState({ live: 1, attention: false }), { state: "running", word: "running" });
  assert.deepEqual(linkState({ live: 0, attention: false }), { state: "", word: "" });
});

test("feed changes add, replace and remove sessions", () => {
  let list = [s("a", "t1", "running")];
  list = applyRailChange(list, { kind: "upsert", session: s("b", "t1", "idle") });
  list = applyRailChange(list, { kind: "upsert", session: s("a", "t1", "done") });
  assert.deepEqual(list.map((x) => `${x.id}:${x.status}`), ["b:idle", "a:done"]);
  list = applyRailChange(list, { kind: "deleted", id: "b" });
  assert.deepEqual(list.map((x) => x.id), ["a"]);
});

test("reviews waiting on the person make the dot say so, even with no session waiting", () => {
  assert.deepEqual(linkState({ live: 1, attention: false }, 1), { state: "waiting", word: "waiting on you" });
  assert.deepEqual(linkState({ live: 0, attention: false }, 2), { state: "waiting", word: "waiting on you" });
  assert.deepEqual(linkState({ live: 1, attention: false }, 0), { state: "running", word: "running" });
});

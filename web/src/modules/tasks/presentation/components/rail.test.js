import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryRail } from "../../infrastructure/memory-rail.js";
import { rail, railLink } from "./rail.js";

file("tasks/presentation/rail");

const s = (id, ticketId, status, pendingApprovals = 0) => ({ id, ticketId, status, pendingApprovals });

function setup() {
  const seeded = [s("a", "t1", "running")];
  const memory = memoryRail({ projectId: "p1", sessions: seeded });
  const store = { byTask: {} };
  const nav = mount(rail({ gateway: memory.gateway, store }), { el: seededElement({ project_id: "p1", sessions: seeded, tasks: [] }, "rail-seed") });
  nav.instance.init();
  const link = (taskId) => {
    const el = document.createElement("a");
    el.dataset.taskId = taskId;
    const m = mount(railLink({ store }), { el });
    m.instance.init();
    return m.instance;
  };
  return { memory, store, nav: nav.instance, link };
}

test("links start from the seed", () => {
  const { link } = setup();
  const t1 = link("t1");
  assert.deepEqual([t1.live, t1.hasCount, t1.dotState], [1, true, "running"]);
  const t2 = link("t2");
  assert.deepEqual([t2.hasCount, t2.hasDot], [false, false]);
});

test("a session started on a task shows on its link as it starts", async () => {
  const { memory, link } = setup();
  const t2 = link("t2");
  await flush();
  memory.emit({ kind: "upsert", session: s("b", "t2", "running") });
  assert.deepEqual([t2.live, t2.dotState, t2.dotWord], [1, "running", "running"]);
  memory.emit({ kind: "upsert", session: s("b", "t2", "idle") });
  assert.deepEqual([t2.dotState, t2.dotWord], ["waiting", "waiting on you"]);
  memory.emit({ kind: "deleted", id: "b" });
  assert.equal(t2.hasDot, false);
});

test("after a resync it reads the sessions again", async () => {
  const { memory, link } = setup();
  const t1 = link("t1");
  await flush();
  memory.replace([s("a", "t1", "done"), s("c", "t3", "running")]);
  memory.status(FeedStatus.RESYNCED);
  await flush();
  assert.equal(t1.hasCount, false);
  assert.equal(link("t3").live, 1);
});

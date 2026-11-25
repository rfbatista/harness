import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { memoryRailFeed, memoryRailSource } from "../infrastructure/memory-rail";
import { session, task } from "../testing/fixtures";
import { activityOf, followProject, type RailUpdate } from "./follow-project";
import type { RailFeedStatus, RailSeed } from "./ports";

const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

function setup(over: Partial<RailSeed> = {}) {
  const seed: RailSeed = { projectId: "p1", tasks: [task()], sessions: [session("a", "t1", "running")], archivedCount: 0, ...over };
  const source = memoryRailSource({ sessions: seed.sessions, tasks: seed.tasks });
  const feed = memoryRailFeed();
  const updates: RailUpdate[] = [];
  const statuses: RailFeedStatus[] = [];
  const follow = followProject({ source, feed, seed, onUpdate: (u) => updates.push(u), onStatus: (s) => statuses.push(s) });
  return { source, feed, updates, statuses, follow };
}

describe("followProject", () => {
  test("starts from the seed, without the archived tasks", () => {
    const { follow } = setup({ tasks: [task(), task({ id: "gone", status: "done", archivedAt: 1 })], archivedCount: 1 });
    assert.deepEqual(follow.state().tasks.map((t) => t.id), ["t1"]);
    assert.equal(follow.state().archivedCount, 1);
    assert.deepEqual(activityOf(follow.state()), { t1: { live: 1, attention: false } });
  });

  test("a ticket change regroups: the task keeps its place, a new one is added, a deleted one goes", () => {
    const { feed, follow, updates } = setup({ tasks: [task(), task({ id: "t2", title: "Two", status: "todo" })] });
    feed.emit({ kind: "task-upsert", task: task({ status: "review" }) });
    assert.deepEqual(follow.state().tasks.map((t) => `${t.id}:${t.status}`), ["t1:review", "t2:todo"]);
    feed.emit({ kind: "task-upsert", task: task({ id: "t3", title: "Three", status: "backlog" }) });
    feed.emit({ kind: "task-deleted", id: "t1" });
    assert.deepEqual(follow.state().tasks.map((t) => t.id), ["t2", "t3"]);
    assert.deepEqual(updates.map((u) => [u.cause, u.before.tasks.length, u.state.tasks.length]), [["change", 2, 2], ["change", 2, 3], ["change", 3, 2]]);
  });

  test("a session change shows on its task's activity, and a deleted session takes it away", () => {
    const { feed, follow } = setup({ tasks: [task(), task({ id: "t2", status: "todo" })], sessions: [] });
    feed.emit({ kind: "upsert", session: session("b", "t2", "idle") });
    assert.deepEqual(activityOf(follow.state()).t2, { live: 1, attention: true });
    feed.emit({ kind: "deleted", id: "b" });
    assert.equal(activityOf(follow.state()).t2, undefined);
  });

  test("an update carries the change and the state before it", () => {
    const { feed, updates } = setup();
    const moved = task({ status: "review" });
    feed.emit({ kind: "task-upsert", task: moved });
    const [u] = updates;
    assert.deepEqual([u?.change, u?.before.tasks[0]?.status, u?.state.tasks[0]?.status], [{ kind: "task-upsert", task: moved }, "in_progress", "review"]);
  });

  test("an archived task leaves at once and the count goes up, once", () => {
    const done = task({ id: "d", status: "done" });
    const { feed, follow } = setup({ tasks: [task(), done], archivedCount: 1 });
    feed.emit({ kind: "task-upsert", task: { ...done, archivedAt: 5 } });
    assert.deepEqual([follow.state().tasks.map((t) => t.id), follow.state().archivedCount], [["t1"], 2]);
    feed.emit({ kind: "task-upsert", task: { ...done, archivedAt: 5 } });
    assert.equal(follow.state().archivedCount, 2, "the same archive again is not counted twice");
  });

  test("a restored task comes back and the archived count and the list are read again", async () => {
    const done = task({ id: "d", status: "done" });
    const { feed, source, follow } = setup({ tasks: [task()], archivedCount: 1 });
    source.setTasks([task(), done]);
    source.setArchived([]);
    feed.emit({ kind: "task-upsert", task: done });
    await flush();
    assert.deepEqual([follow.state().tasks.map((t) => t.id), follow.state().archivedCount], [["t1", "d"], 0]);
  });

  test("a restored feature brings back the children archived on their own, which the feed does not carry", async () => {
    const feature = task({ id: "f", title: "Files tab", status: "done" });
    const kid = task({ id: "k", status: "done", parentId: "f", archivedAt: 3 });
    const { feed, source, follow } = setup({ tasks: [task()] });
    source.setTasks([task(), feature, kid]);
    feed.emit({ kind: "task-upsert", task: feature });
    await flush();
    assert.deepEqual(follow.state().tasks.map((t) => `${t.id}:${Boolean(t.archivedAt)}`).sort(), ["f:false", "k:true", "t1:false"]);
  });

  test("when the feed first goes live it reads the lists again, only that once", async () => {
    const { feed, source, follow, updates } = setup({ tasks: [task({ status: "review" })] });
    source.setTasks([task({ status: "review", mergeState: { state: "not_merged", merged: 0, total: 1 } })]);
    feed.setStatus("live");
    await flush();
    assert.equal(follow.state().tasks[0]?.mergeState?.state, "not_merged");
    assert.ok(updates.every((u) => u.cause === "resync"), "catching up is not news");
    source.setTasks([task({ status: "review", mergeState: { state: "merged", merged: 1, total: 1 } })]);
    feed.setStatus("live");
    await flush();
    assert.equal(follow.state().tasks[0]?.mergeState?.state, "not_merged", "later lives follow the feed");
  });

  test("after a resync it reads sessions and tasks again, and says so", async () => {
    const { feed, source, follow, updates } = setup();
    source.setSessions([session("c", "t9", "idle")]);
    source.setTasks([task({ id: "t9", title: "Elsewhere", status: "done" })]);
    source.setArchived([task({ id: "o", archivedAt: 1 })]);
    feed.setStatus("resynced");
    await flush();
    assert.deepEqual(follow.state().tasks.map((t) => t.id), ["t9"]);
    assert.deepEqual([activityOf(follow.state()).t9, follow.state().archivedCount], [{ live: 1, attention: true }, 1]);
    assert.ok(updates.length > 0 && updates.every((u) => u.cause === "resync"));
  });

  test("a resync refreshes the sessions when the tasks cannot be read, and the other way round", async () => {
    const { feed, source, follow } = setup({ tasks: [task(), task({ id: "t3", status: "todo" })] });
    source.failing("tasks");
    source.setSessions([session("a", "t1", "done"), session("c", "t3", "running")]);
    feed.setStatus("resynced");
    await flush();
    assert.equal(activityOf(follow.state()).t3?.live, 1, "sessions refreshed");
    assert.deepEqual(follow.state().tasks.map((t) => t.id), ["t1", "t3"], "tasks kept as they were");
    source.failing("sessions");
    source.setTasks([task({ id: "z", status: "todo" })]);
    feed.setStatus("resynced");
    await flush();
    assert.deepEqual(follow.state().tasks.map((t) => t.id), ["z"]);
    assert.equal(activityOf(follow.state()).t3?.live, 1, "sessions kept as they were");
  });

  test("it passes the feed's status on, and stops listening when stopped", () => {
    const { feed, statuses, follow, updates } = setup();
    feed.setStatus("paused");
    assert.deepEqual(statuses, ["connecting", "paused"]);
    follow.stop();
    assert.equal(feed.followers(), 0);
    feed.emit({ kind: "task-deleted", id: "t1" });
    assert.equal(updates.length, 0);
  });
});

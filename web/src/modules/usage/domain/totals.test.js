import { assert, file, test } from "../../../shared/testing/test.js";
import { makeRecord, makeTotals } from "../testing/fixtures.js";
import { addLiveRecord, startTotals } from "./totals.js";

file("usage/domain/totals");

const seed = () =>
  startTotals({
    taskId: "t1",
    task: makeTotals({ requests: 2, input: 1000, output: 200, total: 1200, costUsd: 0.5 }),
    sessions: { s1: makeTotals({ requests: 1, input: 600, output: 100, total: 700, costUsd: 0.3 }) },
  });

test("starts from the seed", () => {
  const t = seed();
  assert.equal(t.taskId, "t1");
  assert.equal(t.task.total, 1200);
  assert.equal(t.sessions.s1.total, 700);
});

test("a record of the task adds to the task and to its session", () => {
  const t = addLiveRecord(seed(), makeRecord({ taskId: "t1", sessionId: "s1", input: 100, output: 50, costUsd: 0.1 }));
  assert.deepEqual([t.task.requests, t.task.input, t.task.output, t.task.total], [3, 1100, 250, 1350]);
  assert.equal(Math.round(t.task.costUsd * 100) / 100, 0.6);
  assert.equal(t.sessions.s1.total, 850);
});

test("a new session of the task gets totals of its own", () => {
  const t = addLiveRecord(seed(), makeRecord({ taskId: "t1", sessionId: "s9", input: 10, output: 0, costUsd: null }));
  assert.equal(t.sessions.s9.total, 10);
  assert.equal(t.sessions.s9.costUsd, null, "unpriced from the start: no estimate, not $0");
  assert.equal(t.task.costUsd, 0.5, "an unpriced record leaves the task's estimate alone");
  assert.equal(t.task.unpriced, 10);
});

test("records of other tasks, sessions or the harness are not counted", () => {
  const before = seed();
  for (const r of [
    makeRecord({ taskId: "t2", sessionId: "s2" }),
    makeRecord({ taskId: "", sessionId: "" }),
    makeRecord({ taskId: "", sessionId: "s7" }),
  ]) {
    assert.equal(addLiveRecord(before, r), before);
  }
});

test("a session page without a task counts its own session's records", () => {
  const t = startTotals({ taskId: "", task: null, sessions: { s5: makeTotals() } });
  const next = addLiveRecord(t, makeRecord({ taskId: "", sessionId: "s5", input: 7, output: 0 }));
  assert.equal(next.sessions.s5.total, 7);
  assert.equal(next.task, null);
});

test("a record already counted is ignored", () => {
  const r = makeRecord({ taskId: "t1", sessionId: "s1" });
  const once = addLiveRecord(seed(), r);
  assert.equal(addLiveRecord(once, { ...r }), once);
  assert.equal(once.task.requests, 3);
});

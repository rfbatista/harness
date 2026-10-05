import { assert, file, test } from "../../../shared/testing/test.js";
import { preferredRun, runState } from "./run.js";

file("runs/domain/run");

const at = (s) => new Date(`2026-10-05T12:00:0${s}Z`);

test("the run to show: the newest running one, else the newest", () => {
  const runs = [
    { id: "c", status: "exited", startedAt: at(3) },
    { id: "b", status: "running", startedAt: at(2) },
    { id: "a", status: "running", startedAt: at(1) },
  ];
  assert.equal(preferredRun(runs).id, "b");
  assert.equal(preferredRun([runs[0]]).id, "c");
  assert.equal(preferredRun([]), null);
});

test("how a run's state reads", () => {
  assert.deepEqual(runState({ status: "running" }), { state: "running", word: "running" });
  assert.deepEqual(runState({ status: "stopped" }), { state: "done", word: "stopped" });
  assert.deepEqual(runState({ status: "exited", exitCode: 0 }), { state: "done", word: "exited" });
  assert.deepEqual(runState({ status: "exited", exitCode: 1 }), { state: "failed", word: "exited (code 1)" });
});

import { assert, file, test } from "../../../shared/testing/test.js";
import { statusChangeView } from "./statusChange.js";

file("tasks/presentation/statusChange");

const now = new Date("2026-10-02T14:02:00Z");
const change = (fields) => ({
  taskId: "t1",
  status: "review",
  reason: "",
  by: "session",
  bySessionId: "s-arch",
  at: new Date("2026-10-02T14:00:00Z"),
  ...fields,
});

test("the architect's move names it, says when and quotes why", () => {
  assert.deepEqual(statusChangeView(change({ reason: "Server and tools merged" }), { architectSessionId: "s-arch", now }), {
    line: "Moved to review by the architect · 2m",
    reason: "“Server and tools merged”",
  });
});

test("another session's or a person's move says so; no time, no reason, no trailing words", () => {
  assert.equal(
    statusChangeView(change({ bySessionId: "s-peer" }), { architectSessionId: "s-arch", now }).line,
    "Moved to review by a session · 2m",
  );
  assert.deepEqual(
    statusChangeView(change({ by: "person", bySessionId: "", status: "in_progress", at: null }), { architectSessionId: "s-arch", now }),
    {
      line: "Moved to in progress by a person",
      reason: "",
    },
  );
});

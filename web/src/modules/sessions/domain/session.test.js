import { assert, file, test } from "../../../shared/testing/test.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import { applyChange, group, needsYou } from "./session.js";

file("sessions/domain/session");

const at = (minutes) => new Date(T0.getTime() + minutes * 60_000);

test("a changed session moves to the top; a deleted one leaves", () => {
  const a = makeSession({ id: "a" });
  const b = makeSession({ id: "b" });
  const b2 = makeSession({ id: "b", status: "done" });
  let list = applyChange([a, b], { kind: "upsert", session: b2 });
  assert.deepEqual(list.map((s) => `${s.id}:${s.status}`), ["b:done", "a:running"]);
  list = applyChange(list, { kind: "deleted", id: "a" });
  assert.deepEqual(list.map((s) => s.id), ["b"]);
});

test("an idle interactive session is the developer's turn; a finished one never is", () => {
  assert.ok(needsYou(makeSession({ status: "idle" })));
  assert.ok(needsYou(makeSession({ status: "running", pendingApprovals: 1 })));
  assert.ok(!needsYou(makeSession({ status: "failed", pendingApprovals: 1 })));
  assert.ok(!needsYou(makeSession({ status: "thinking" })));
});

test("groups for triage, most recent first, without empty groups", () => {
  const groups = group([
    makeSession({ id: "old-run", status: "running", updatedAt: at(1) }),
    makeSession({ id: "done", status: "done", updatedAt: at(9) }),
    makeSession({ id: "new-run", status: "thinking", updatedAt: at(5) }),
    makeSession({ id: "turn", status: "idle", updatedAt: at(2) }),
  ]);
  assert.deepEqual(
    groups.map((g) => [g.key, g.sessions.map((s) => s.id)]),
    [["needs-you", ["turn"]], ["active", ["new-run", "old-run"]], ["finished", ["done"]]],
  );
  assert.deepEqual(group([makeSession({ status: "done" })]).map((g) => g.key), ["finished"]);
});

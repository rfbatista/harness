import { assert, file, test } from "../../../shared/testing/test.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import roles from "../../../../testdata/views/session-roles.json" with { type: "json" };
import { applyChange, canResume, displayOrder, group, needsYou, ofTask } from "./session.js";

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

test("a task's list keeps only its own sessions, and drops one that stops matching", () => {
  const mine = makeSession({ id: "mine", ticketId: "t1" });
  const keep = ofTask("t1");
  let list = applyChange([mine], { kind: "upsert", session: makeSession({ id: "other", ticketId: "t2" }) }, keep);
  assert.deepEqual(list.map((s) => s.id), ["mine"], "another task's session is ignored");

  list = applyChange(list, { kind: "upsert", session: makeSession({ id: "second", ticketId: "t1" }) }, keep);
  assert.deepEqual(list.map((s) => s.id), ["second", "mine"], "a task runs several sessions");

  list = applyChange(list, { kind: "upsert", session: makeSession({ id: "mine", ticketId: "t9" }) }, keep);
  assert.deepEqual(list.map((s) => s.id), ["second"]);
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

test("only a session the server marks resumable can be resumed", () => {
  assert.equal(canResume(makeSession({ status: "stopped", resumable: true, resumeBlocked: "" })), true);
  assert.equal(canResume(makeSession({ status: "done", resumable: false, resumeBlocked: "WORKSPACE_MISSING" })), false);
  assert.equal(canResume(makeSession()), false, "a running session is not resumable");
});

// Roles: the architect and its delegates. The shared fixture pins the order
// for the BFF too (internal/adapter/in/web/sessions/view_test.go).

const fromFixture = (s) =>
  makeSession({ id: s.id, role: s.role, parentSessionId: s.parent ?? "", status: s.status, updatedAt: new Date(T0.getTime() - s.ago * 60_000) });

test("the architect leads its group with its delegates under it (shared fixture)", () => {
  for (const c of roles.cases) {
    const groups = group(c.sessions.map(fromFixture));
    assert.deepEqual(
      groups.map((g) => [g.key, g.sessions.map((s) => [s.id, g.depth?.[s.id] ?? 0])]),
      c.groups,
      c.name,
    );
  }
});

test("the display order is the groups' order, for J and K", () => {
  const c = roles.cases[1];
  assert.deepEqual(displayOrder(c.sessions.map(fromFixture)), ["arch", "web", "server", "server-tests", "deep", "deeper", "peer"]);
});

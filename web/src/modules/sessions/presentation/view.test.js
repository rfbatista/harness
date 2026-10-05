import fixture from "../../../../testdata/views/session-status.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { needsYou } from "../domain/session.js";
import { makeSession, T0 } from "../testing/fixtures.js";
import { statusView, summary, toGroupViews } from "./view.js";

file("sessions/presentation/view");

test("status words match the shared fixture (parity with the BFF)", () => {
  for (const c of fixture.cases) {
    const session = makeSession({ status: c.status, pendingApprovals: c.pending_approvals });
    const label = `${c.status}+${c.pending_approvals}`;
    assert.deepEqual(statusView(session), { state: c.state, word: c.word }, label);
    assert.equal(needsYou(session), c.needs_you, `${label} needs_you`);
  }
});

test("group views carry labels, counts and the selection", () => {
  const now = new Date(T0.getTime() + 5 * 60_000);
  const groups = toGroupViews(
    [makeSession({ id: "a", status: "idle" }), makeSession({ id: "b", task: "" })],
    { selectedId: "b", now },
  );
  assert.deepEqual(groups.map((g) => `${g.label}:${g.count}`), ["Needs you:1", "Running:1"]);
  assert.deepEqual(groups.map((g) => g.tone), ["attention", null]);
  const row = groups[1].rows[0];
  assert.deepEqual([row.title, row.meta, row.selected, row.attention], ["Untitled session", "backend · 5m", true, false]);
});

test("summary counts what is waiting", () => {
  assert.equal(summary([makeSession({ status: "idle" }), makeSession({ id: "b" })]), "2 sessions · 1 waiting");
  assert.equal(summary([makeSession()]), "1 session");
});

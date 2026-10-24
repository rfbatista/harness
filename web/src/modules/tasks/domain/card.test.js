import views from "../../../../testdata/views/task-card.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { age, exactTime, lastMoveLine, mergeBadge } from "./card.js";

file("tasks/domain/card");

const now = new Date(views.now);

/** A fixture's last_move as the task model holds it. */
const lastMove = (m) =>
  m && { status: m.status, by: m.by, sessionId: m.session_id ?? "", sessionLabel: m.session_label ?? "", reason: m.reason ?? "", at: Date.parse(m.at) };

test("the time in column reads as the board says it, stale past its column's threshold", () => {
  for (const c of views.ages) {
    const got = age(makeTask({ status: c.status, statusSince: Date.parse(c.since) }), now);
    assert.deepEqual({ text: got.text, stale: got.stale }, { text: c.age, stale: c.stale }, `${c.status} since ${c.since}`);
  }
});

test("a task whose time in column is unknown shows no age", () => {
  assert.deepEqual(age(makeTask({ statusSince: 0 }), now), { text: "", stale: false, since: null });
});

test("the last move line says who moved the task here, or who created it", () => {
  for (const c of views.lastMoves) {
    const got = lastMoveLine(makeTask({ lastMove: lastMove(c.last_move) }));
    assert.deepEqual({ lead: got.lead, who: got.who, reason: got.reason }, { lead: c.lead, who: c.who, reason: c.reason }, JSON.stringify(c.last_move));
  }
});

test("a task from an old event, with no last move field at all, reads as created by you", () => {
  const { lastMove: _, ...old } = makeTask();
  assert.deepEqual(lastMoveLine(old), { lead: "Created by", who: "you", reason: "" });
});

test("the merge badge shows on Review and Done only, never for none or a missing merge state", () => {
  for (const c of views.merges) {
    const mergeState = c.merge_state && { state: c.merge_state.state, merged: c.merge_state.merged, total: c.merge_state.total };
    assert.deepEqual(mergeBadge(makeTask({ status: c.status, mergeState })), c.badge, `${c.status} ${JSON.stringify(c.merge_state)}`);
  }
  const { mergeState: _, ...old } = makeTask({ status: "review" });
  assert.equal(mergeBadge(old), null, "an old event without the field");
  assert.equal(mergeBadge(makeTask({ status: "done", mergeState: { state: "from-the-future", merged: 0, total: 0 } })), null, "a state this client does not know");
});

test("the exact time reads as month, day and a 24-hour clock, in local time", () => {
  assert.equal(exactTime(new Date(2026, 9, 19, 9, 12).getTime()), "Oct 19, 09:12");
  assert.equal(exactTime(new Date(2026, 0, 3, 21, 5).getTime()), "Jan 3, 21:05");
});

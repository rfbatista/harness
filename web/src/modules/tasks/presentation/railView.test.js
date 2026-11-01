import cards from "../../../../testdata/views/feature-cards.json" with { type: "json" };
import { assert, file, test } from "../../../shared/testing/test.js";
import { activityByTask } from "../domain/activity.js";
import { onBoard } from "../domain/board.js";
import { railGroups } from "../domain/rail.js";
import { makeTask } from "../testing/fixtures.js";
import { toGroupView, toRow } from "./railView.js";

file("tasks/presentation/railView");

const ctx = (over = {}) => ({ projectId: "p1", byTask: {}, currentId: "", fresh: new Set(), signal: new Set(), focusedId: "", query: "", ...over });

test("a row's meta is one item: the reviews badge, else waiting, else N live", () => {
  const worst = toRow(makeTask({ pendingReviews: 2 }), ctx({ byTask: { t1: { live: 3, attention: true } } }));
  assert.deepEqual([worst.hasReviews, worst.reviewsText, worst.isWaiting, worst.isLive, worst.state, worst.attention], [true, "2 reviews", false, false, "waiting", true]);
  const waiting = toRow(makeTask(), ctx({ byTask: { t1: { live: 1, attention: true } } }));
  assert.deepEqual([waiting.hasReviews, waiting.isWaiting, waiting.isLive, waiting.lead, waiting.tail], [false, true, false, ", ", " on you"]);
  const running = toRow(makeTask(), ctx({ byTask: { t1: { live: 2, attention: false } } }));
  assert.deepEqual([running.isLive, running.liveText, running.lead, running.tail, running.state, running.attention], [true, "2 live", ", running, ", " sessions", "running", false]);
  const one = toRow(makeTask(), ctx({ byTask: { t1: { live: 1, attention: false } } }));
  assert.equal(one.tail, " session");
  const quiet = toRow(makeTask(), ctx());
  assert.deepEqual([quiet.hasDot, quiet.hasReviews, quiet.isWaiting, quiet.isLive, quiet.lead, quiet.tail], [false, false, false, false, "", ""]);
  const review = toRow(makeTask({ pendingReviews: 1 }), ctx());
  assert.deepEqual([review.reviewsText, review.lead, review.tail], ["1 review", ", ", " waiting on you"]);
});

test("a quiet Review or Done row shows the merge glyph and word, and its state hook only when there is one", () => {
  const partial = toRow(makeTask({ status: "review", mergeState: { state: "partial", merged: 1, total: 2 } }), ctx());
  assert.deepEqual([partial.hasMerge, partial.mergeState, partial.mergeText, partial.lead, partial.tail], [true, "partial", "1/2 merged", ", ", ""]);
  const live = toRow(makeTask({ status: "done", mergeState: { state: "merged", merged: 1, total: 1 } }), ctx({ byTask: { t1: { live: 1, attention: false } } }));
  assert.deepEqual([live.isLive, live.hasMerge, live.mergeState], [true, false, null]);
  const none = toRow(makeTask({ status: "done", mergeState: { state: "none", merged: 0, total: 0 } }), ctx());
  assert.deepEqual([none.hasMerge, none.mergeState, none.mergeText, none.lead], [false, null, "", ""]);
});

test("a row links its task, marks it current, fresh, signalled or peeking, and splits the title around the match", () => {
  const row = toRow(makeTask({ title: "Add SSE feed" }), ctx({ currentId: "t1", fresh: new Set(["t1"]), signal: new Set(["t1"]), focusedId: "t1", query: "sse" }));
  assert.deepEqual([row.href, row.current, row.fresh, row.signal, row.peek], ["/projects/p1/tasks/t1", "page", true, true, true]);
  assert.deepEqual([row.before, row.match, row.after, row.hasMatch], ["Add ", "SSE", " feed", true]);
  const other = toRow(makeTask({ id: "t2" }), ctx({ currentId: "t1" }));
  assert.deepEqual([other.current, other.fresh, other.signal, other.peek, other.hasMatch], [false, false, false, false, false]);
});

test("a heading says its count in words, and what needs the person", () => {
  const tasks = [makeTask({ id: "b1", status: "backlog", pendingReviews: 1 }), makeTask({ id: "b2", status: "backlog" })];
  const [, review, , backlog] = railGroups(tasks, { collapsed: ["backlog"] }).map((g) => toGroupView(g, ctx()));
  assert.deepEqual([backlog.label, backlog.countText, backlog.countWords, backlog.foldable, backlog.expanded, backlog.collapsed], ["Backlog", "2", " tasks, 1 needs you", true, false, true]);
  assert.deepEqual([backlog.headingId, backlog.listId], ["rail-backlog-heading", "rail-backlog"]);
  assert.deepEqual([review.countText, review.countWords, review.foldable, review.empty, review.hasRows], ["0", " tasks", false, true, false]);
  const [, , , filtered] = railGroups(tasks, { query: "zzz" }).map((g) => toGroupView(g, ctx({ query: "zzz" })));
  assert.deepEqual([filtered.countText, filtered.countWords, filtered.foldable], ["0 of 2", " tasks match", false]);
  const emptyWhileFiltering = toGroupView(railGroups(tasks, { query: "zzz" })[1], ctx({ query: "zzz" }));
  assert.deepEqual([emptyWhileFiltering.countText, emptyWhileFiltering.countWords], ["0", " tasks"], "an empty group stays 0, not 0 of 0");
});

test("Done's rest reads Show N more", () => {
  const tasks = Array.from({ length: 8 }, (_, n) => makeTask({ id: `d${n}`, status: "done", updatedAt: n }));
  const done = toGroupView(railGroups(tasks, {}).at(-1), ctx());
  assert.deepEqual([done.rows.length, done.more.length, done.hasMore, done.moreText], [5, 3, true, "Show 3 more"]);
});

test("the rail nests children under their feature as the shared fixture says (parity with the BFF)", () => {
  const tasks = cards.tasks.map((t) =>
    makeTask({
      id: t.id,
      title: t.title,
      status: t.status,
      parentId: t.parent_id,
      statusSince: Date.parse("2026-10-19T08:00:00Z") + t.since * 60000,
      pendingReviews: t.reviews ?? 0,
      archivedAt: t.archived ? Date.parse("2026-10-01T12:00:00Z") : 0,
      mergeState: t.merge ? { state: t.merge, merged: 1, total: 1 } : null,
    }),
  );
  const byTask = activityByTask(cards.sessions.map((s) => ({ id: `s-${s.task_id}`, ticketId: s.task_id, status: s.status, pendingApprovals: 0 })));
  const meta = (row) => (row.hasReviews ? `reviews:${row.reviewsText}` : row.isWaiting ? "waiting" : row.isLive ? `live:${row.liveText}` : row.hasMerge ? `merge:${row.mergeText}` : row.hasStatusWord ? `status:${row.statusWord}` : "");
  for (const g of railGroups(onBoard(tasks), { byTask }).map((g) => toGroupView(g, ctx({ byTask })))) {
    const got = g.rows.map((r) => `${r.id}=${meta(r)}[${r.kids.map((k) => `${k.id}=${meta(k)}`).join(" ")}]`);
    const want = cards.rail[g.key].map((r) => `${r.id}=${r.meta}[${r.kids.map((k) => `${k.id}=${k.meta}`).join(" ")}]`);
    assert.deepEqual(got, want, g.key);
    assert.equal(g.rows.length, want.length, `${g.key}: children are not tasks of a group`);
  }
});

test("a feature row opens to its children, and its signals cover them only while it is folded", () => {
  const tasks = [makeTask({ id: "f", status: "todo" }), makeTask({ id: "c", status: "todo", parentId: "f", pendingReviews: 2 })];
  const group = (open) => toGroupView(railGroups(tasks, {}).find((g) => g.key === "todo"), ctx({ open: () => open }));
  const [folded] = group(false).rows;
  assert.deepEqual([folded.isFeature, folded.open, folded.openText, folded.reviewsText, folded.twistyLabel, folded.kidsCount], [true, false, "false", "2 reviews", "Expand t1, 1 task".replace("t1", "Add SSE feed"), "1"]);
  const [open] = group(true).rows;
  assert.deepEqual([open.open, open.hasReviews, open.twistyLabel, open.kids[0].reviewsText], [true, false, "Collapse Add SSE feed, 1 task", "2 reviews"]);
});

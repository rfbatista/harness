import { assert, file, test } from "../../../shared/testing/test.js";
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

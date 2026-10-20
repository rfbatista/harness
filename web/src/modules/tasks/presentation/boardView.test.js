import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { toColumns } from "./boardView.js";

file("tasks/presentation/boardView");

const now = new Date("2026-10-19T12:00:00Z");
const ctx = (over = {}) => ({ projectId: "p1", byTask: {}, fresh: new Set(), now, ...over });

test("columns in board order, cards with their links, newest move on top", () => {
  const tasks = [
    makeTask({ id: "t1", statusSince: Date.parse("2026-10-18T12:00:00Z") }),
    makeTask({ id: "t 2", title: "Write <docs>", status: "todo" }),
    makeTask({ id: "t3", statusSince: Date.parse("2026-10-19T11:00:00Z") }),
  ];
  const cols = toColumns(tasks, ctx({ projectId: "p/1", fresh: new Set(["t 2"]) }));
  assert.deepEqual(cols.map((c) => `${c.status}:${c.label}:${c.count}:${c.isEmpty}`), ["backlog:Backlog:0:true", "todo:Todo:1:false", "in_progress:In progress:2:false", "review:Review:0:true", "done:Done:0:true"]);
  assert.deepEqual(cols[2].cards.map((c) => c.id), ["t3", "t1"]);
  const [docs] = cols[1].cards;
  assert.deepEqual([docs.href, docs.status, docs.fresh], ["/projects/p%2F1/tasks/t%202", "todo", true]);
});

test("the signals row: a waiting session, else the live count; the reviews badge; nothing when quiet", () => {
  const tasks = [makeTask({ id: "w", pendingReviews: 1 }), makeTask({ id: "l" }), makeTask({ id: "q" }), makeTask({ id: "r", pendingReviews: 2 })];
  const byTask = { w: { live: 1, attention: true }, l: { live: 2, attention: false } };
  const cards = Object.fromEntries(toColumns(tasks, ctx({ byTask }))[2].cards.map((c) => [c.id, c]));
  assert.deepEqual([cards.w.liveState, cards.w.liveWord, cards.w.reviews, cards.w.hasSignals], ["waiting", "waiting", "1 review", true]);
  assert.deepEqual([cards.l.liveState, cards.l.liveWord, cards.l.reviews, cards.l.hasSignals], ["running", "2 live", "", true]);
  assert.deepEqual([cards.r.liveState, cards.r.reviews, cards.r.hasSignals], ["", "2 reviews", true]);
  assert.deepEqual([cards.q.liveState, cards.q.hasLive, cards.q.hasReviews, cards.q.hasMerge, cards.q.hasSignals], ["", false, false, false, false]);
});

test("a Review card carries its merge badge, its last move, its time in column and its reason", () => {
  const task = makeTask({
    status: "review",
    statusSince: Date.parse("2026-10-17T11:00:00Z"),
    lastMove: { status: "review", by: "session", sessionId: "s", sessionLabel: "go-developer", reason: "All tests pass.", at: Date.parse("2026-10-17T11:00:00Z") },
    mergeState: { state: "partial", merged: 1, total: 2 },
  });
  const [card] = toColumns([task], ctx())[3].cards;
  assert.deepEqual([card.hasMerge, card.mergeState, card.mergeWord], [true, "partial", "1 of 2 merged"]);
  assert.deepEqual([card.lead, card.who, card.reason, card.hasReason], ["Moved here by", "go-developer", "All tests pass.", true]);
  assert.deepEqual([card.age, card.stale], ["2d · stale", true]);
  assert.ok(card.at.startsWith(" · Oct 17, "), card.at);
  assert.ok(card.ageTitle.startsWith("In this column since Oct 17, "), card.ageTitle);
});

test("a card from an old event reads as created by you, with no badge and no age", () => {
  const [card] = toColumns([makeTask({ status: "done" })], ctx())[4].cards;
  assert.deepEqual([card.lead, card.who, card.hasMerge, card.mergeState, card.age, card.at, card.hasReason], ["Created by", "you", false, "", "", "", false]);
});

test("Review asks for the person's check while it holds cards; empty columns say so in their own words", () => {
  const empty = toColumns([], ctx());
  assert.deepEqual(empty.map((c) => [c.waiting, c.sub, c.emptyText]), [
    [false, "", "No tasks"],
    [false, "", "No tasks"],
    [false, "", "No tasks"],
    [false, "", "Nothing waiting for your check."],
    [false, "", "No done tasks. Drag a checked task here from Review."],
  ]);
  const review = toColumns([makeTask({ status: "review" })], ctx())[3];
  assert.deepEqual([review.waiting, review.sub], [true, "Waiting for your check"]);
});

test("a card with a move in flight is pending and says where it goes; one that went back says the move failed", () => {
  const cols = toColumns([makeTask({ id: "a", status: "done" }), makeTask({ id: "b", status: "review" }), makeTask({ id: "c", status: "review" })], ctx({ pending: { a: "done" }, failed: new Set(["b"]) }));
  const [a] = cols[4].cards;
  const [b, c] = cols[3].cards;
  assert.deepEqual([a.pending, a.failed, a.note], [true, false, "Moving to Done…"]);
  assert.deepEqual([b.pending, b.failed, b.note], [false, true, "Move failed"]);
  assert.deepEqual([c.pending, c.failed, c.note], [false, false, ""]);
});

test("each card's Move to… menu lists the statuses, its own checked, and opens on the next step", () => {
  const [card] = toColumns([makeTask({ status: "review" })], ctx())[3].cards;
  assert.deepEqual(card.choices.map((c) => `${c.key}:${c.label}:${c.current}:${c.initial}`), ["1:Backlog:false:false", "2:Todo:false:false", "3:In progress:false:false", "4:Review:true:false", "5:Done:false:true"]);
});

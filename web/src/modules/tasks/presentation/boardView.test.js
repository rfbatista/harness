import { assert, file, test } from "../../../shared/testing/test.js";
import { makeTask } from "../testing/fixtures.js";
import { archiveAllView, jumpChips, toColumns } from "./boardView.js";

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

test("Archive all: the button shows while Done holds cards; the strip names the count, live, in each state", () => {
  assert.deepEqual(archiveAllView({ phase: "rest", done: 14 }), { shows: true, open: false, phase: "rest", title: "", body: "", confirm: "", emptyText: "", asking: false, busy: false, failed: false, tone: null });
  assert.equal(archiveAllView({ phase: "rest", done: 0 }).shows, false);
  const confirm = archiveAllView({ phase: "confirm", done: 14 });
  assert.deepEqual([confirm.open, confirm.title, confirm.body, confirm.confirm], [true, "Archive all 14 done tasks?", "They leave the board and the rail. Nothing is lost: restore any of them from Archived.", "Archive 14"]);
  assert.deepEqual([archiveAllView({ phase: "confirm", done: 15 }).confirm, archiveAllView({ phase: "confirm", done: 1 }).title], ["Archive 15", "Archive all 1 done task?"], "the count follows Done while the strip is open");
  assert.deepEqual([archiveAllView({ phase: "confirm", done: 0 }).open, archiveAllView({ phase: "confirm", done: 0 }).phase], [false, "rest"], "Done emptied: the strip closes");
  assert.deepEqual([archiveAllView({ phase: "busy", done: 14 }).title, archiveAllView({ phase: "busy", done: 14 }).open], ["Archiving 14…", true]);
  const failed = archiveAllView({ phase: "failed", done: 14 });
  assert.deepEqual([failed.open, failed.title], [true, "Couldn't archive. Nothing changed."]);
  const done = archiveAllView({ phase: "done", done: 0, archived: 14 });
  assert.deepEqual([done.open, done.shows, done.emptyText], [false, false, "14 tasks archived."]);
  assert.equal(archiveAllView({ phase: "done", done: 1, archived: 14 }).emptyText, "", "a card landed in Done meanwhile: the column shows it instead");
});

test("while a card is dragged: its place holds a ghost, the column under it previews the count and says where it goes", () => {
  const tasks = [makeTask({ id: "a", status: "review" }), makeTask({ id: "b", status: "done" })];
  const cols = toColumns(tasks, ctx({ drag: { id: "a", from: "review", over: "done" }, fine: true }));
  const review = cols[3];
  const done = cols[4];
  assert.deepEqual([review.dragOrigin, review.dropOver, review.dropSelf, review.cards[0].ghost], [true, false, false, true]);
  assert.deepEqual([done.dropOver, done.countText, done.dropText], [true, "1 → 2", "Drop to move to Done"]);
  assert.equal(cols[0].countText, "0", "other columns keep their count");
  const self = toColumns(tasks, ctx({ drag: { id: "a", from: "review", over: "review" }, fine: true }))[3];
  assert.deepEqual([self.dropSelf, self.dropOver, self.selfText, self.countText], [true, false, "Already in Review", "1"]);
});

test("cards drag on a fine pointer only, and never while their move is in flight", () => {
  const tasks = [makeTask({ id: "a" }), makeTask({ id: "b" })];
  const fine = toColumns(tasks, ctx({ fine: true, pending: { b: "in_progress" } }))[2].cards;
  assert.deepEqual(fine.map((c) => `${c.id}:${c.draggable}`).sort(), ["a:true", "b:false"]);
  assert.deepEqual(toColumns(tasks, ctx({ fine: false }))[2].cards.map((c) => c.draggable), [false, false]);
});

test("the narrow screen's jump chips: one link per column with its count; Review asks for a look while it holds cards; a bumped chip rings", () => {
  const cols = toColumns([makeTask({ id: "a", status: "review" }), makeTask({ id: "b", status: "done" }), makeTask({ id: "c", status: "done" })], ctx());
  const chips = jumpChips(cols, { bumped: "done" });
  assert.deepEqual(chips.map((c) => `${c.href}|${c.label}|${c.count}|${c.waiting}|${c.bumped}`), [
    "#column-backlog|Backlog|0|false|false",
    "#column-todo|Todo|0|false|false",
    "#column-in_progress|In progress|0|false|false",
    "#column-review|Review|1|true|false",
    "#column-done|Done|2|false|true",
  ]);
  assert.equal(jumpChips(toColumns([], ctx()), { bumped: "" })[3].waiting, false, "an empty Review is quiet");
});

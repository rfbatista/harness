import { assert, file, test } from "../../../shared/testing/test.js";
import { at, makeRecord, makeSummary, makeTotals, MIDNIGHT } from "../testing/fixtures.js";
import { chart, feedRow, modelSlots, sessionRows, taskRows, totalTiles } from "./view.js";

file("usage/presentation/view");

test("totals show each token kind and the estimated cost", () => {
  const tiles = totalTiles(makeTotals({ requests: 3, input: 1200, output: 340, cacheWrite: 0, cacheRead: 2_500_000, total: 2_501_540, costUsd: null, unpriced: 2_501_540 }));
  assert.deepEqual(
    tiles.map((t) => `${t.label}=${t.value}`),
    ["Total tokens=2.5M", "Input=1.2K", "Output=340", "Cache write=0", "Cache read=2.5M", "Requests=3", "Estimated cost=—"],
  );
  assert.equal(tiles[0].exact, "2,501,540 tokens");
});

test("the chart fills every bucket of the range, stacked by model", () => {
  const today = makeSummary({
    series: [
      { start: at(10), model: "b-model", input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 300 },
      { start: at(10), model: "a-model", input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 100 },
      { start: at(13), model: "a-model", input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 50 },
    ],
  });
  const slots = modelSlots({}, today.series);
  const c = chart(today, slots);
  assert.equal(c.columns.length, 24, "today: one bucket per hour");
  assert.equal(c.columns[0].start.getTime(), MIDNIGHT.getTime());
  assert.deepEqual(c.columns[10].segments.map((s) => `${s.model}:${s.total}`), ["a-model:100", "b-model:300"], "stacked in slot order");
  assert.equal(c.columns[10].total, 400);
  assert.equal(c.columns[11].segments.length, 0, "an empty hour is still a column");
  assert.equal(c.max, 400);
  assert.deepEqual(c.legend.map((l) => `${l.model}:${l.slot}`), ["a-model:1", "b-model:2"]);
  assert.ok(c.columns[10].label.length > 0);

  const week = chart(makeSummary({ range: "7d", bucket: "day", since: new Date(2026, 9, 8), series: [] }), slots);
  assert.equal(week.columns.length, 7);
  assert.equal(week.columns[6].start.getTime(), MIDNIGHT.getTime());
  assert.equal(chart(makeSummary({ range: "30d", bucket: "day", since: new Date(2026, 8, 15), series: [] }), slots).columns.length, 30);
  assert.equal(week.empty, true);
});

test("a model keeps its colour when the range changes; past eight they share Other", () => {
  let slots = modelSlots({}, [{ model: "b" }, { model: "a" }]);
  assert.deepEqual(slots, { a: 1, b: 2 });
  slots = modelSlots(slots, [{ model: "b" }, { model: "c" }]);
  assert.deepEqual(slots, { a: 1, b: 2, c: 3 });
  for (const m of ["d", "e", "f", "g", "h", "i", "j"]) slots = modelSlots(slots, [{ model: m }]);
  assert.equal(slots.h, 8);
  assert.equal(slots.i, 0, "slot 0 is Other");
  assert.equal(slots.j, 0);
});

test("top tasks and sessions link to their pages", () => {
  const s = makeSummary({
    topSessions: [
      { sessionId: "s 1", taskId: "t1", taskTitle: "Add the feed", totals: makeTotals({ total: 1200, costUsd: 0.5 }) },
      { sessionId: "s2", taskId: "", taskTitle: "", totals: makeTotals({ total: 5, costUsd: null, unpriced: 5 }) },
    ],
  });
  const tasks = taskRows(s.topTasks, "p 1", s.totals.total);
  assert.deepEqual([tasks[0].title, tasks[0].href, tasks[0].tokens, tasks[0].cost], ["Add the feed", "/projects/p%201/tasks/t1", "1.2K", "$0.50"]);
  assert.equal(tasks[0].share, 100);
  const sessions = sessionRows(s.topSessions, "p 1", s.totals.total);
  assert.equal(sessions[0].href, "/projects/p%201/tasks/t1?session=s%201");
  assert.equal(sessions[0].task, "Add the feed");
  assert.equal(sessions[1].href, "", "a session without a task has no page");
  assert.equal(sessions[1].cost, "—");
  assert.equal(taskRows([{ taskId: "t9", taskTitle: "", totals: makeTotals() }], "p1", 0)[0].title, "Untitled task");
});

test("a feed row says when, what model, which task, how many tokens and the estimated cost", () => {
  const row = feedRow(makeRecord({ id: "r1", input: 1000, output: 234, costUsd: null, durationMs: 2500 }), "p1");
  assert.equal(row.id, "r1");
  assert.equal(row.tokens, "1.2K");
  assert.equal(row.cost, "—");
  assert.equal(row.taskHref, "/projects/p1/tasks/t1");
  assert.equal(row.duration, "2.5s");
  assert.ok(row.time.length > 0);
  assert.equal(row.failed, false);
  assert.equal(feedRow(makeRecord({ status: 529, error: "overloaded" }), "p1").failed, true);
  assert.equal(feedRow(makeRecord({ taskId: "", taskTitle: "", sessionId: "" }), "p1").task, "Harness", "the harness's own spend");
  assert.equal(feedRow(makeRecord({ taskId: "", taskTitle: "" }), "p1").task, "No task", "a session without a task");
  assert.equal(feedRow(makeRecord({ taskId: "", taskTitle: "", sessionId: "", attribution: "harness:writing-helper" }), "p1").task, "Writing helper", "the writing helper's spend");
  assert.equal(feedRow(makeRecord({ taskId: "", taskTitle: "", sessionId: "", attribution: "harness:system" }), "p1").task, "Harness");
});

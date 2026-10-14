import { assert, file, test } from "../../../shared/testing/test.js";
import { at, makeRecord, makeSummary, makeTotals } from "../testing/fixtures.js";
import { applyRecord, FEED_CAP } from "./usage.js";

file("usage/domain/usage");

const start = () => ({ summary: makeSummary(), records: [] });

test("a live record joins the feed first and the totals of the current range", () => {
  const r = makeRecord({ input: 100, output: 50, cacheWrite: 10, cacheRead: 40, costUsd: 0.25 });
  const next = applyRecord(start(), r);
  assert.deepEqual(next.records.map((x) => x.id), [r.id]);
  const t = next.summary.totals;
  assert.deepEqual([t.requests, t.input, t.output, t.cacheWrite, t.cacheRead, t.total], [3, 1100, 250, 10, 40, 1400]);
  assert.equal(t.costUsd, 0.75);
});

test("it adds to its model's bucket, or opens one", () => {
  let s = applyRecord(start(), makeRecord({ startedAt: at(10, 40), input: 10, output: 0 }));
  s = applyRecord(s, makeRecord({ startedAt: at(10, 50), model: "claude-opus-4-1", input: 5, output: 0 }));
  s = applyRecord(s, makeRecord({ startedAt: at(11, 2), input: 7, output: 0 }));
  const point = (h, model) => s.summary.series.find((p) => p.start.getTime() === at(h).getTime() && p.model === model);
  assert.equal(point(10, "claude-sonnet-4-5").total, 1210);
  assert.equal(point(10, "claude-sonnet-4-5").input, 1010);
  assert.equal(point(10, "claude-opus-4-1").total, 5);
  assert.equal(point(11, "claude-sonnet-4-5").total, 7);
  assert.equal(s.summary.series.length, 3);
});

test("day ranges bucket by local day", () => {
  const summary = makeSummary({ range: "7d", bucket: "day", series: [] });
  const s = applyRecord({ summary, records: [] }, makeRecord({ startedAt: at(22, 30), input: 3, output: 0 }));
  assert.equal(s.summary.series[0].start.getTime(), at(0).getTime());
});

test("the matching task and session grow; new ones join the top lists, ranked by tokens", () => {
  let s = applyRecord(start(), makeRecord({ taskId: "t1", sessionId: "s1", input: 100, output: 0 }));
  assert.equal(s.summary.topTasks[0].totals.total, 1300);
  assert.equal(s.summary.topSessions[0].totals.total, 1300);

  s = applyRecord(s, makeRecord({ taskId: "t2", taskTitle: "Ship it", sessionId: "s2", input: 5000, output: 0, costUsd: 1 }));
  assert.deepEqual(s.summary.topTasks.map((t) => `${t.taskId}:${t.totals.total}`), ["t2:5000", "t1:1300"]);
  assert.equal(s.summary.topTasks[0].taskTitle, "Ship it");
  assert.equal(s.summary.topTasks[0].totals.costUsd, 1);
  assert.deepEqual(s.summary.topSessions.map((x) => `${x.sessionId}:${x.taskId}`), ["s2:t2", "s1:t1"]);
});

test("the harness's own spend has no task or session to rank", () => {
  const s = applyRecord(start(), makeRecord({ taskId: "", sessionId: "", taskTitle: "" }));
  assert.deepEqual([s.summary.topTasks.length, s.summary.topSessions.length], [1, 1]);
  assert.equal(s.summary.totals.requests, 3);
});

test("an unpriced record leaves the estimated cost alone and counts as unpriced", () => {
  let s = applyRecord(start(), makeRecord({ model: "mystery", costUsd: null, input: 10, output: 0 }));
  assert.deepEqual([s.summary.totals.costUsd, s.summary.totals.unpriced], [0.5, 10]);

  const empty = makeSummary({ totals: makeTotals(), series: [], topTasks: [], topSessions: [] });
  s = applyRecord({ summary: empty, records: [] }, makeRecord({ model: "mystery", costUsd: null, input: 10, output: 0 }));
  assert.equal(s.summary.totals.costUsd, null, "nothing priced at all: no estimate, not $0");
  assert.equal(s.summary.topTasks[0].totals.costUsd, null);
  s = applyRecord(s, makeRecord({ costUsd: 0.2, input: 1, output: 0 }));
  assert.equal(s.summary.totals.costUsd, 0.2, "the first priced record starts the estimate");
});

test("the feed keeps the newest records up to its cap", () => {
  let s = start();
  const ids = [];
  for (let i = 0; i < FEED_CAP + 3; i++) {
    const r = makeRecord();
    ids.unshift(r.id);
    s = applyRecord(s, r);
  }
  assert.equal(s.records.length, FEED_CAP);
  assert.deepEqual(s.records.map((r) => r.id), ids.slice(0, FEED_CAP));
  assert.equal(applyRecord({ summary: null, records: [] }, makeRecord(), 2).records.length, 1, "a smaller cap is honoured");
});

test("a record already shown is ignored", () => {
  const r = makeRecord();
  const once = applyRecord(start(), r);
  const twice = applyRecord(once, { ...r });
  assert.equal(twice, once);
  assert.equal(twice.summary.totals.requests, 3);
});

test("a record from before the range starts goes to the feed but not the range's numbers", () => {
  const before = makeRecord({ startedAt: new Date(at(0).getTime() - 60_000) });
  const s = applyRecord(start(), before);
  assert.equal(s.records[0].id, before.id);
  assert.equal(s.summary.totals.requests, 2);
  assert.equal(s.summary.series.length, 1);
  assert.equal(s.summary.topTasks[0].totals.total, 1200);
});

test("without a summary yet, records still reach the feed", () => {
  const s = applyRecord({ summary: null, records: [] }, makeRecord());
  assert.deepEqual([s.summary, s.records.length], [null, 1]);
});

test("a record after the range's last day ends the range: it joins the feed, the numbers wait for a reload", () => {
  const tomorrow = new Date(2026, 9, 15, 0, 5);
  const today = applyRecord(start(), makeRecord({ startedAt: tomorrow, input: 9, output: 0 }));
  assert.equal(today.stale, true);
  assert.equal(today.records.length, 1);
  assert.equal(today.summary.totals.input, 1000);

  const week = { summary: makeSummary({ range: "7d", bucket: "day", since: new Date(2026, 9, 8) }), records: [] };
  assert.equal(applyRecord(week, makeRecord({ startedAt: tomorrow })).stale, true);
  assert.equal(applyRecord(week, makeRecord({ startedAt: at(23, 59) })).stale, undefined);
});

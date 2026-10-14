import { Codes } from "../../../shared/domain/errors.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { toRecord, toRecords, toSeed, toSummary } from "./dto.js";

file("usage/infrastructure/dto");

const wireTotals = {
  requests: 2,
  input_tokens: 1000,
  output_tokens: 200,
  cache_creation_input_tokens: 30,
  cache_read_input_tokens: 400,
  total_tokens: 1630,
  estimated_cost_usd: 0.5,
  unpriced_tokens: 0,
};

const wireSummary = {
  range: "today",
  since: "2026-10-14T00:00:00-03:00",
  bucket: "hour",
  totals: wireTotals,
  series: [
    {
      start: "2026-10-14T10:00:00-03:00",
      model: "claude-sonnet-4-5",
      input_tokens: 1000,
      output_tokens: 200,
      cache_creation_input_tokens: 30,
      cache_read_input_tokens: 400,
      total_tokens: 1630,
    },
  ],
  top_tasks: [{ task_id: "t1", task_title: "Add the feed", totals: wireTotals }],
  top_sessions: [{ session_id: "s1", task_id: "t1", task_title: "Add the feed", totals: { ...wireTotals, estimated_cost_usd: null, unpriced_tokens: 1630 } }],
};

const wireRecord = {
  id: "req-1",
  attribution: "s1",
  project_id: "p1",
  task_id: "t1",
  task_title: "Add the feed",
  session_id: "s1",
  method: "POST",
  path: "/v1/messages",
  model: "claude-sonnet-4-5",
  status: 200,
  streamed: true,
  started_at: "2026-10-14T13:05:00Z",
  duration_ms: 1500,
  input_tokens: 100,
  output_tokens: 50,
  cache_creation_input_tokens: 10,
  cache_read_input_tokens: 40,
  error: "",
  estimated_cost_usd: 0.01,
};

test("a summary maps to the domain's names, with times as dates", () => {
  const s = toSummary(wireSummary);
  assert.deepEqual([s.range, s.bucket], ["today", "hour"]);
  assert.equal(s.since.toISOString(), "2026-10-14T03:00:00.000Z");
  assert.deepEqual(s.totals, { requests: 2, input: 1000, output: 200, cacheWrite: 30, cacheRead: 400, total: 1630, costUsd: 0.5, unpriced: 0 });
  assert.equal(s.series[0].start.toISOString(), "2026-10-14T13:00:00.000Z");
  assert.deepEqual([s.series[0].model, s.series[0].cacheRead, s.series[0].total], ["claude-sonnet-4-5", 400, 1630]);
  assert.deepEqual([s.topTasks[0].taskId, s.topTasks[0].taskTitle, s.topTasks[0].totals.total], ["t1", "Add the feed", 1630]);
  assert.deepEqual([s.topSessions[0].sessionId, s.topSessions[0].taskId], ["s1", "t1"]);
  assert.equal(s.topSessions[0].totals.costUsd, null, "an unpriced cost stays null, never 0");
});

test("empty lists may come as null", () => {
  const s = toSummary({ ...wireSummary, series: null, top_tasks: null, top_sessions: null });
  assert.deepEqual([s.series, s.topTasks, s.topSessions], [[], [], []]);
});

test("a record maps to the domain's names, with its total", () => {
  const r = toRecord(wireRecord);
  assert.equal(r.startedAt.toISOString(), "2026-10-14T13:05:00.000Z");
  assert.deepEqual([r.id, r.taskId, r.taskTitle, r.sessionId, r.model, r.durationMs], ["req-1", "t1", "Add the feed", "s1", "claude-sonnet-4-5", 1500]);
  assert.deepEqual([r.input, r.output, r.cacheWrite, r.cacheRead, r.total, r.costUsd], [100, 50, 10, 40, 200, 0.01]);
  assert.equal(toRecord({ ...wireRecord, estimated_cost_usd: null }).costUsd, null);
  assert.equal(toRecord({ ...wireRecord, task_id: undefined, session_id: undefined }).taskId, "");
  assert.ok(Object.isFrozen(r));
  assert.equal("attribution" in r, false, "fields the page does not use stay behind");
});

test("a page of records and the page's seed", () => {
  assert.deepEqual(toRecords({ records: [wireRecord] }).map((r) => r.id), ["req-1"]);
  assert.deepEqual(toRecords({ records: null }), []);
  const seed = toSeed({ project_id: "p1", summary: wireSummary, records: [wireRecord] });
  assert.equal(seed.projectId, "p1");
  assert.equal(seed.summary.totals.total, 1630);
  assert.equal(seed.records[0].id, "req-1");
  const bare = toSeed({ project_id: "p1", summary: null, records: null });
  assert.deepEqual([bare.summary, bare.records], [null, []]);
});

test("a malformed summary is BAD_RESPONSE", () => {
  assert.throws(() => toSummary(null), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, range: "forever" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, bucket: "minute" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, since: "yesterday" }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, totals: undefined }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, totals: { ...wireTotals, input_tokens: "1000" } }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, totals: { ...wireTotals, estimated_cost_usd: "0.5" } }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, series: {} }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, series: [{ ...wireSummary.series[0], start: "noon" }] }), Codes.BAD_RESPONSE);
  assert.throws(() => toSummary({ ...wireSummary, top_tasks: [{ task_title: "no id", totals: wireTotals }] }), Codes.BAD_RESPONSE);
});

test("a malformed record is BAD_RESPONSE", () => {
  assert.throws(() => toRecord({ ...wireRecord, id: "" }), Codes.BAD_RESPONSE);
  assert.throws(() => toRecord({ ...wireRecord, started_at: "soon" }), Codes.BAD_RESPONSE);
  assert.throws(() => toRecord({ ...wireRecord, output_tokens: null }), Codes.BAD_RESPONSE);
  assert.throws(() => toRecord({ ...wireRecord, estimated_cost_usd: "free" }), Codes.BAD_RESPONSE);
  assert.throws(() => toRecords([wireRecord]), Codes.BAD_RESPONSE);
  assert.throws(() => toSeed({ summary: null, records: [] }), Codes.BAD_RESPONSE);
});

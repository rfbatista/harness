// Domain usage shapes for tests and the dev pages.

/** Local midnight of the day the fixtures happen on. */
export const MIDNIGHT = new Date(2026, 9, 14, 0, 0, 0, 0);
export const at = (hour, minute = 0) => new Date(2026, 9, 14, hour, minute, 0, 0);

/** @returns {import("../domain/usage.js").Totals} */
export function makeTotals(over = {}) {
  return { requests: 0, input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 0, costUsd: 0, unpriced: 0, ...over };
}

let seq = 0;

/** @returns {import("../domain/usage.js").UsageRecord} */
export function makeRecord(over = {}) {
  const r = {
    id: `req-${++seq}`,
    projectId: "p1",
    taskId: "t1",
    taskTitle: "Add the feed",
    sessionId: "s1",
    model: "claude-sonnet-4-5",
    status: 200,
    streamed: true,
    startedAt: at(10, 5),
    durationMs: 1500,
    input: 100,
    output: 50,
    cacheWrite: 0,
    cacheRead: 0,
    costUsd: 0.01,
    error: "",
    ...over,
  };
  return { ...r, total: r.input + r.output + r.cacheWrite + r.cacheRead };
}

/** Today's summary: one model at 10:00, one task, one session. */
/** @returns {import("../domain/usage.js").UsageSummary} */
export function makeSummary(over = {}) {
  const totals = makeTotals({ requests: 2, input: 1000, output: 200, total: 1200, costUsd: 0.5 });
  return {
    range: "today",
    since: MIDNIGHT,
    bucket: "hour",
    totals,
    series: [{ start: at(10), model: "claude-sonnet-4-5", input: 1000, output: 200, cacheWrite: 0, cacheRead: 0, total: 1200 }],
    topTasks: [{ taskId: "t1", taskTitle: "Add the feed", totals }],
    topSessions: [{ sessionId: "s1", taskId: "t1", taskTitle: "Add the feed", totals }],
    ...over,
  };
}

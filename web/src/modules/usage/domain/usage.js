// Token usage as the Usage page holds it (the Glossary's "Usage monitoring"):
// a project's summary over a range, and the live feed of usage records. A
// record arriving live is merged into both, the way the server would have
// counted it, so the page never reloads to stay current.

/**
 * Token sums with their estimated cost: null when there were tokens and none
 * had a price. unpriced is how many tokens the cost leaves out.
 * @typedef {{ requests: number, input: number, output: number, cacheWrite: number, cacheRead: number,
 *             total: number, costUsd: number|null, unpriced: number }} Totals
 *
 * One model's tokens in one bucket, which starts at start.
 * @typedef {{ start: Date, model: string, input: number, output: number, cacheWrite: number, cacheRead: number, total: number }} SeriesPoint
 *
 * @typedef {{ taskId: string, taskTitle: string, totals: Totals }} TaskUsage
 * @typedef {{ sessionId: string, taskId: string, taskTitle: string, totals: Totals }} SessionUsage
 *
 * @typedef {"today"|"7d"|"30d"} Range
 * @typedef {"hour"|"day"} Bucket
 *
 * @typedef {{ range: Range, since: Date, bucket: Bucket, totals: Totals, series: SeriesPoint[],
 *             topTasks: TaskUsage[], topSessions: SessionUsage[] }} UsageSummary
 *
 * One finished LLM request. taskId and sessionId are "" for the harness's own
 * spend; costUsd is null when its model has no price.
 * @typedef {{ id: string, projectId: string, taskId: string, taskTitle: string, sessionId: string, model: string,
 *             status: number, streamed: boolean, startedAt: Date, durationMs: number,
 *             input: number, output: number, cacheWrite: number, cacheRead: number, total: number,
 *             costUsd: number|null, error: string }} UsageRecord
 *
 * What the page shows: summary is null until it is read.
 * @typedef {{ summary: UsageSummary|null, records: UsageRecord[] }} UsageState
 */

/** @type {Range[]} */
export const RANGES = ["today", "7d", "30d"];
export const BUCKETS = ["hour", "day"];

/** How many records the live feed keeps. */
export const FEED_CAP = 50;
/** How many tasks and sessions the summary ranks (the server's topN). */
export const TOP_N = 10;

/** @returns {Totals} */
export const emptyTotals = () => ({ requests: 0, input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 0, costUsd: 0, unpriced: 0 });

/** Where the bucket holding `at` starts, in local time. */
export function bucketStart(at, bucket) {
  const d = new Date(at.getFullYear(), at.getMonth(), at.getDate());
  if (bucket === "hour") d.setHours(at.getHours());
  return d;
}

/**
 * totals with r counted. Priced tokens add to the estimate; unpriced ones
 * are left out of it, and with nothing priced at all there is no estimate.
 * @param {Totals} t @param {UsageRecord} r @returns {Totals}
 */
export function addToTotals(t, r) {
  const next = {
    requests: t.requests + 1,
    input: t.input + r.input,
    output: t.output + r.output,
    cacheWrite: t.cacheWrite + r.cacheWrite,
    cacheRead: t.cacheRead + r.cacheRead,
    total: t.total + r.total,
    costUsd: t.costUsd,
    unpriced: t.unpriced,
  };
  if (r.costUsd !== null) {
    next.costUsd = (t.costUsd ?? 0) + r.costUsd;
  } else {
    next.unpriced += r.total;
    const pricedBefore = t.costUsd !== null && t.total > t.unpriced;
    if (!pricedBefore && next.unpriced > 0) next.costUsd = null;
  }
  return next;
}

/**
 * The state with a live record merged in: first in the feed (capped), and
 * counted in the summary when it falls inside its range. A record already
 * in the feed changes nothing: the same state comes back.
 * @param {UsageState} state @param {UsageRecord} r @param {number} [cap]
 * @returns {UsageState}
 */
export function applyRecord(state, r, cap = FEED_CAP) {
  if (state.records.some((x) => x.id === r.id)) return state;
  const records = [r, ...state.records].slice(0, cap);
  const s = state.summary;
  if (!s || r.startedAt.getTime() < s.since.getTime()) return { summary: s, records };
  return {
    records,
    summary: {
      ...s,
      totals: addToTotals(s.totals, r),
      series: addToSeries(s.series, r, s.bucket),
      topTasks: r.taskId ? rank(s.topTasks, "taskId", r.taskId, () => ({ taskId: r.taskId, taskTitle: r.taskTitle }), r) : s.topTasks,
      topSessions: r.sessionId
        ? rank(s.topSessions, "sessionId", r.sessionId, () => ({ sessionId: r.sessionId, taskId: r.taskId, taskTitle: r.taskTitle }), r)
        : s.topSessions,
    },
  };
}

/** @param {SeriesPoint[]} series @param {UsageRecord} r @param {Bucket} bucket */
function addToSeries(series, r, bucket) {
  const start = bucketStart(r.startedAt, bucket);
  const i = series.findIndex((p) => p.start.getTime() === start.getTime() && p.model === r.model);
  const base = i >= 0 ? series[i] : { start, model: r.model, input: 0, output: 0, cacheWrite: 0, cacheRead: 0, total: 0 };
  const point = {
    ...base,
    input: base.input + r.input,
    output: base.output + r.output,
    cacheWrite: base.cacheWrite + r.cacheWrite,
    cacheRead: base.cacheRead + r.cacheRead,
    total: base.total + r.total,
  };
  if (i >= 0) return series.map((p, j) => (j === i ? point : p));
  return [...series, point].sort((a, b) => a.start.getTime() - b.start.getTime());
}

/** The list with r counted on the entry keyed `id`, added when new, most tokens first. */
function rank(list, key, id, fresh, r) {
  const found = list.some((e) => e[key] === id);
  const next = found
    ? list.map((e) => (e[key] === id ? { ...e, totals: addToTotals(e.totals, r) } : e))
    : [...list, { ...fresh(), totals: addToTotals(emptyTotals(), r) }];
  return next.sort((a, b) => b.totals.total - a.totals.total || (a[key] < b[key] ? -1 : 1)).slice(0, TOP_N);
}

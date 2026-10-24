// The Usage page as the markup binds it: total tiles, the tokens-over-time
// chart's columns, the top task and session rows, and the live feed's rows.

import { costView, estimatedCost, tokenCount, tokens } from "../domain/format.js";

/** How many models get a colour of their own; the rest share slot 0, Other. */
export const SERIES_SLOTS = 8;

const enc = encodeURIComponent;
const taskHref = (projectId, taskId) => (taskId ? `/projects/${enc(projectId)}/tasks/${enc(taskId)}` : "");

/** @param {import("../domain/usage.js").Totals} t */
export function totalTiles(t) {
  const tile = (key, label, n) => ({ key, label, value: tokens(n), exact: `${tokenCount(n)} tokens`, note: "" });
  const cost = costView(t);
  return [
    tile("total", "Total tokens", t.total),
    tile("input", "Input", t.input),
    tile("output", "Output", t.output),
    tile("cache-write", "Cache write", t.cacheWrite),
    tile("cache-read", "Cache read", t.cacheRead),
    { key: "requests", label: "Requests", value: tokenCount(t.requests), exact: `${tokenCount(t.requests)} requests`, note: "" },
    { key: "cost", label: cost.label, value: cost.text, exact: cost.aria, note: cost.note },
  ];
}

/**
 * Each model's colour slot (1–8), kept for the page's life so a model keeps
 * its colour when the range changes. New models take the next free slot in
 * name order; past eight they share 0, Other.
 * @param {Record<string, number>} slots @param {{ model: string }[]} series
 */
export function modelSlots(slots, series) {
  const next = { ...slots };
  const fresh = [...new Set(series.map((p) => p.model))].filter((m) => !(m in next)).sort();
  let used = Object.values(next).filter((s) => s > 0).length;
  for (const m of fresh) next[m] = used < SERIES_SLOTS ? ++used : 0;
  return next;
}

const COLUMNS = { today: 24, "7d": 7, "30d": 30 };
const TICK_EVERY = { today: 6, "7d": 1, "30d": 5 };

const hourLabel = (d) => `${String(d.getHours()).padStart(2, "0")}:00`;
const dayLabel = (d) => d.toLocaleDateString("en-US", { month: "short", day: "numeric" });

/**
 * The tokens-over-time chart: one column per bucket across the whole range,
 * empty ones included, each stacked by model in slot order (Other last).
 * @param {import("../domain/usage.js").UsageSummary} summary
 * @param {Record<string, number>} slots
 */
export function chart(summary, slots) {
  const n = COLUMNS[summary.range] ?? 0;
  const hourly = summary.bucket === "hour";
  const order = (slot) => (slot === 0 ? SERIES_SLOTS + 1 : slot);
  const columns = [];
  for (let i = 0; i < n; i++) {
    const start = new Date(summary.since);
    if (hourly) start.setHours(start.getHours() + i);
    else start.setDate(start.getDate() + i);
    const byModel = new Map();
    for (const p of summary.series) {
      if (p.start.getTime() !== start.getTime()) continue;
      const slot = slots[p.model] ?? 0;
      const model = slot === 0 ? "Other" : p.model;
      const seg = byModel.get(model) ?? { model, slot, total: 0 };
      seg.total += p.total;
      byModel.set(model, seg);
    }
    const segments = [...byModel.values()].sort((a, b) => order(a.slot) - order(b.slot));
    const total = segments.reduce((sum, s) => sum + s.total, 0);
    const label = hourly ? hourLabel(start) : dayLabel(start);
    columns.push({
      key: start.getTime(),
      start,
      label,
      tick: i % TICK_EVERY[summary.range] === 0,
      /** What the axis shows under the column: every few columns' label. */
      axis: i % TICK_EVERY[summary.range] === 0 ? label : "",
      total,
      totalText: tokens(total),
      segments,
      byModel: segments.map((s) => `${s.model} ${tokens(s.total)}`).join(", "),
      title: `${label}: ${total ? segments.map((s) => `${s.model} ${tokens(s.total)}`).join(", ") : "no tokens"}`,
    });
  }
  const max = Math.max(0, ...columns.map((c) => c.total));
  const models = new Map();
  for (const p of summary.series) {
    const slot = slots[p.model] ?? 0;
    const model = slot === 0 ? "Other" : p.model;
    const entry = models.get(model) ?? { model, slot, total: 0 };
    entry.total += p.total;
    models.set(model, entry);
  }
  const legend = [...models.values()]
    .sort((a, b) => order(a.slot) - order(b.slot))
    .map((l) => ({ ...l, tokens: tokens(l.total) }));
  return { columns, max, maxLabel: tokens(max), legend, empty: max === 0 };
}

const COST_TITLE = "Estimated cost";

const share = (n, of) => (of > 0 ? Math.round((n / of) * 100) : 0);

/** @param {import("../domain/usage.js").TaskUsage[]} list */
export function taskRows(list, projectId, rangeTotal) {
  return list.map((t) => ({
    id: t.taskId,
    title: t.taskTitle || "Untitled task",
    href: taskHref(projectId, t.taskId),
    tokens: tokens(t.totals.total),
    exact: `${tokenCount(t.totals.total)} tokens`,
    cost: estimatedCost(t.totals.costUsd),
    costTitle: COST_TITLE,
    share: share(t.totals.total, rangeTotal),
    shareText: `${share(t.totals.total, rangeTotal)}%`,
  }));
}

/** @param {import("../domain/usage.js").SessionUsage[]} list */
export function sessionRows(list, projectId, rangeTotal) {
  return list.map((s) => {
    const task = taskHref(projectId, s.taskId);
    return {
      id: s.sessionId,
      label: `Session ${s.sessionId.slice(0, 8)}`,
      task: s.taskId ? s.taskTitle || "Untitled task" : "No task",
      href: task ? `${task}?session=${enc(s.sessionId)}` : "",
      tokens: tokens(s.totals.total),
      exact: `${tokenCount(s.totals.total)} tokens`,
      cost: estimatedCost(s.totals.costUsd),
      costTitle: COST_TITLE,
      share: share(s.totals.total, rangeTotal),
      shareText: `${share(s.totals.total, rangeTotal)}%`,
    };
  });
}

const clockTime = (d) => [d.getHours(), d.getMinutes(), d.getSeconds()].map((x) => String(x).padStart(2, "0")).join(":");

/** What the harness's own spend is called: the writing helper's has a name, the rest is the harness's. */
const harnessSpend = (attribution) => (attribution === "harness:writing-helper" ? "Writing helper" : "Harness");

/** @param {import("../domain/usage.js").UsageRecord} r */
export function feedRow(r, projectId) {
  let task = r.taskTitle || "Untitled task";
  if (!r.taskId) task = r.sessionId ? "No task" : harnessSpend(r.attribution);
  const failed = r.error !== "" || r.status >= 400;
  return {
    id: r.id,
    time: clockTime(r.startedAt),
    iso: r.startedAt.toISOString(),
    model: r.model || "unknown model",
    task,
    taskHref: taskHref(projectId, r.taskId),
    tokens: tokens(r.total),
    exact: `${tokenCount(r.total)} tokens: ${tokenCount(r.input)} input, ${tokenCount(r.output)} output, ${tokenCount(r.cacheWrite)} cache write, ${tokenCount(r.cacheRead)} cache read`,
    cost: estimatedCost(r.costUsd),
    duration: `${(r.durationMs / 1000).toFixed(1)}s`,
    failed,
    /** data-failed on the row: present or absent. */
    failedAttr: failed ? "" : null,
    status: r.error || (r.status >= 400 ? `HTTP ${r.status}` : ""),
  };
}

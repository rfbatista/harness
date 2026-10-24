// The wire format of token usage (GET /api/projects/{p}/usage,
// /usage/records, the /usage/events stream and the Usage page's seed) and its
// mapping to the domain's shapes.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { BUCKETS, RANGES } from "../domain/usage.js";

/** @returns {import("../domain/usage.js").Totals} */
export function toTotals(dto, at = "totals") {
  if (!dto || typeof dto !== "object") bad(`${at} missing`);
  return {
    requests: num(dto.requests, `${at}.requests`),
    input: num(dto.input_tokens, `${at}.input_tokens`),
    output: num(dto.output_tokens, `${at}.output_tokens`),
    cacheWrite: num(dto.cache_creation_input_tokens, `${at}.cache_creation_input_tokens`),
    cacheRead: num(dto.cache_read_input_tokens, `${at}.cache_read_input_tokens`),
    total: num(dto.total_tokens, `${at}.total_tokens`),
    costUsd: cost(dto.estimated_cost_usd, `${at}.estimated_cost_usd`),
    unpriced: num(dto.unpriced_tokens ?? 0, `${at}.unpriced_tokens`),
  };
}

/** @returns {import("../domain/usage.js").UsageSummary} */
export function toSummary(dto) {
  if (!dto || typeof dto !== "object") bad("expected a usage summary");
  if (!RANGES.includes(dto.range)) bad(`unknown range "${dto.range}"`);
  if (!BUCKETS.includes(dto.bucket)) bad(`unknown bucket "${dto.bucket}"`);
  return {
    range: dto.range,
    since: date(dto.since, "since"),
    bucket: dto.bucket,
    totals: toTotals(dto.totals),
    series: list(dto.series, "series").map((p, i) => {
      const at = `series[${i}]`;
      return {
        start: date(p?.start, `${at}.start`),
        model: str(p.model),
        input: num(p.input_tokens, `${at}.input_tokens`),
        output: num(p.output_tokens, `${at}.output_tokens`),
        cacheWrite: num(p.cache_creation_input_tokens, `${at}.cache_creation_input_tokens`),
        cacheRead: num(p.cache_read_input_tokens, `${at}.cache_read_input_tokens`),
        total: num(p.total_tokens, `${at}.total_tokens`),
      };
    }),
    topTasks: list(dto.top_tasks, "top_tasks").map((t, i) => ({
      taskId: id(t?.task_id, `top_tasks[${i}]`),
      taskTitle: str(t.task_title),
      totals: toTotals(t.totals, `top_tasks[${i}].totals`),
    })),
    topSessions: list(dto.top_sessions, "top_sessions").map((s, i) => ({
      sessionId: id(s?.session_id, `top_sessions[${i}]`),
      taskId: str(s.task_id),
      taskTitle: str(s.task_title),
      totals: toTotals(s.totals, `top_sessions[${i}].totals`),
    })),
  };
}

/** One usage record, as the records page and the events stream carry it. */
/** @returns {import("../domain/usage.js").UsageRecord} */
export function toRecord(dto) {
  if (!dto || typeof dto !== "object") bad("expected a usage record");
  const at = `record ${id(dto.id, "record")}`;
  const r = {
    id: dto.id,
    projectId: str(dto.project_id),
    attribution: str(dto.attribution),
    taskId: str(dto.task_id),
    taskTitle: str(dto.task_title),
    sessionId: str(dto.session_id),
    model: str(dto.model),
    status: num(dto.status ?? 0, `${at}: status`),
    streamed: dto.streamed === true,
    startedAt: date(dto.started_at, `${at}: started_at`),
    durationMs: num(dto.duration_ms ?? 0, `${at}: duration_ms`),
    input: num(dto.input_tokens, `${at}: input_tokens`),
    output: num(dto.output_tokens, `${at}: output_tokens`),
    cacheWrite: num(dto.cache_creation_input_tokens, `${at}: cache_creation_input_tokens`),
    cacheRead: num(dto.cache_read_input_tokens, `${at}: cache_read_input_tokens`),
    total: 0,
    costUsd: cost(dto.estimated_cost_usd, `${at}: estimated_cost_usd`),
    error: str(dto.error),
  };
  r.total = r.input + r.output + r.cacheWrite + r.cacheRead;
  return Object.freeze(r);
}

/** GET /api/projects/{p}/usage/records → {"records": [...]}, newest first. */
export function toRecords(body) {
  if (!body || typeof body !== "object" || Array.isArray(body) || !("records" in body)) bad("expected {records: [...]}");
  return list(body.records, "records").map(toRecord);
}

/**
 * The Usage page's seed: {"project_id", "summary", "records"}. summary is
 * null when the server could not read it; the page reads it then.
 */
export function toSeed(body) {
  if (!body || typeof body.project_id !== "string" || body.project_id === "") bad("seed without a project_id");
  return {
    projectId: body.project_id,
    summary: body.summary ? toSummary(body.summary) : null,
    records: list(body.records, "records").map(toRecord),
  };
}

/**
 * A task page's usage totals seed: {"project_id", "task_id", "task",
 * "sessions": {id: totals}}, all-time. task is null on a page without a task
 * or when its totals were not read.
 */
export function toTotalsSeed(body) {
  if (!body || typeof body.project_id !== "string" || body.project_id === "") bad("totals seed without a project_id");
  const sessions = body.sessions ?? {};
  if (typeof sessions !== "object" || Array.isArray(sessions)) bad("totals seed sessions is not a map");
  return {
    projectId: body.project_id,
    taskId: str(body.task_id),
    task: body.task ? toTotals(body.task, "task") : null,
    sessions: Object.fromEntries(Object.entries(sessions).map(([id, t]) => [id, toTotals(t, `sessions.${id}`)])),
  };
}

const str = (v) => (typeof v === "string" ? v : "");

function id(v, at) {
  if (typeof v !== "string" || v === "") bad(`${at} without an id`);
  return v;
}

function list(v, at) {
  if (v === null || v === undefined) return [];
  if (!Array.isArray(v)) bad(`${at} is not a list`);
  return v;
}

function num(v, at) {
  if (typeof v !== "number" || !Number.isFinite(v)) bad(`${at} is not a number`);
  return v;
}

function cost(v, at) {
  if (v === null || v === undefined) return null;
  return num(v, at);
}

function date(v, at) {
  const d = typeof v === "string" && v !== "" ? new Date(v) : null;
  if (!d || Number.isNaN(d.getTime())) bad(`${at} is not a time`);
  return d;
}

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected usage: ${detail}.`);
}

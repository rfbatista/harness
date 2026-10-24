// The live call log's domain: call entries held in memory from the live call
// stream (contract: harness server <-> web UI). A `call.started` event adds an
// in-flight entry, `call.finished` completes it in place, `calls.missed` adds a
// missed calls marker. Pure: no DOM, no network, no storage. Every function
// returns new values and leaves its arguments untouched.

/** How many call entries the log holds; the oldest go first. */
export const MAX_ENTRIES = 500;

/**
 * A call entry: one LLM call, merged from its started and finished events.
 * @typedef {object} CallEntry
 * @property {"call"} type
 * @property {string} id
 * @property {Date} startedAt
 * @property {string} attribution
 * @property {string} projectId  "" for calls with no project
 * @property {{id: string, title: string}|null} session
 * @property {{id: string, title: string}|null} task
 * @property {string} kind  "session" | "system" | "document-assistant" | "writing-helper"
 * @property {{method: string, path: string, query: string, headers: object, body: unknown}} request
 * @property {boolean} finished
 * @property {boolean} noLongerInFlight  unfinished, and its finish may have been missed
 * @property {number} status  0 when upstream was never reached or still in flight
 * @property {boolean} streamed
 * @property {string} model
 * @property {string} error
 * @property {number} durationMs
 * @property {Date|null} endedAt
 * @property {object|null} usage
 * @property {{headers: object, body: unknown, events: object[]|null}|null} response  null until finished
 * @property {object} json  the entry's full JSON as the server sent it, started and finished merged
 */

/**
 * A missed calls marker: where the server could not deliver calls.
 * @typedef {object} MissedMarker
 * @property {"missed"} type
 * @property {string} id
 * @property {number} count
 * @property {Date} at
 */

/**
 * @typedef {object} LiveLog
 * @property {(CallEntry|MissedMarker)[]} items  oldest first
 * @property {number} missed  the sum of every marker's count, including dropped markers
 * @property {number} markers  markers ever added, for their ids
 */

/** @returns {LiveLog} */
export const emptyLog = () => ({ items: [], missed: 0, markers: 0 });

/** Empties the log, the missed calls total included. @returns {LiveLog} */
export const clearLog = () => emptyLog();

/** Whether the entry's call is still running as far as the view knows. */
export const isInFlight = (entry) => !entry.finished && !entry.noLongerInFlight;

/**
 * @param {LiveLog} log
 * @param {string} type  a live call stream event name
 * @param {any} data  the event's parsed data
 * @returns {LiveLog}
 */
export function applyEvent(log, type, data) {
  switch (type) {
    case "call.started":
      return withEntry(log, startEntry(data));
    case "call.finished":
      return finish(log, data);
    case "calls.missed":
      return addMarker(log, data);
    default:
      return log;
  }
}

function startEntry(d) {
  return {
    type: "call",
    id: d.id,
    startedAt: new Date(d.started_at),
    attribution: d.attribution ?? "",
    projectId: d.project_id ?? "",
    session: d.session ?? null,
    task: d.task ?? null,
    kind: d.kind ?? "",
    request: d.request,
    finished: false,
    noLongerInFlight: false,
    status: 0,
    streamed: false,
    model: "",
    error: "",
    durationMs: 0,
    endedAt: null,
    usage: null,
    response: null,
    json: d,
  };
}

function withEntry(log, entry) {
  let items = [...log.items, entry];
  if (items.filter((i) => i.type === "call").length > MAX_ENTRIES) {
    // drop the oldest entry, and any marker that came before it
    items = items.slice(items.findIndex((i) => i.type === "call") + 1);
  }
  return { ...log, items };
}

function finish(log, d) {
  const at = log.items.findIndex((i) => i.type === "call" && i.id === d.id);
  if (at < 0) return log;
  const e = log.items[at];
  const done = {
    ...e,
    finished: true,
    noLongerInFlight: false,
    status: d.status ?? 0,
    streamed: d.streamed ?? false,
    model: d.model ?? "",
    error: d.error ?? "",
    durationMs: d.duration_ms ?? 0,
    endedAt: new Date(d.ended_at),
    usage: d.usage ?? null,
    response: d.response ?? null,
    json: { ...e.json, ...d },
  };
  const items = log.items.slice();
  items[at] = done;
  return { ...log, items };
}

function addMarker(log, d) {
  const markers = log.markers + 1;
  const items = log.items.map((i) => (i.type === "call" && isInFlight(i) ? { ...i, noLongerInFlight: true } : i));
  items.push({ type: "missed", id: `missed-${markers}`, count: d.count, at: new Date(d.at) });
  return { items, missed: log.missed + d.count, markers };
}

/** @param {LiveLog} log @returns {CallEntry[]} */
export const entriesOf = (log) => log.items.filter((i) => i.type === "call");

// search

/**
 * @typedef {object} Match
 * @property {(string|number)[]} path  keys and array indexes from the entry's JSON root
 * @property {"key"|"value"} where
 */

function walk(value, path, q, found, first) {
  if (Array.isArray(value)) {
    for (let i = 0; i < value.length && !(first && found.length); i++) walk(value[i], [...path, i], q, found, first);
  } else if (value !== null && typeof value === "object") {
    for (const [k, v] of Object.entries(value)) {
      if (first && found.length) return;
      const here = [...path, k];
      if (k.toLowerCase().includes(q)) found.push({ path: here, where: "key" });
      walk(v, here, q, found, first);
    }
  } else if (String(value).toLowerCase().includes(q)) {
    found.push({ path, where: "value" });
  }
}

/**
 * Where a free-text search matches in the entry's full JSON: keys and values,
 * ignoring case. A blank search reports none.
 * @param {CallEntry} entry
 * @param {string} query
 * @returns {Match[]}
 */
export function searchMatches(entry, query) {
  const q = (query ?? "").trim().toLowerCase();
  const found = [];
  if (q) walk(entry.json, [], q, found, false);
  return found;
}

// filters

/**
 * @typedef {object} LiveFilter  every field optional; an empty one filters nothing
 * @property {string} [query]  free text, anywhere in the entry's JSON
 * @property {string} [session]  a session id
 * @property {string} [task]  a task id
 * @property {string} [model]
 * @property {""|"2xx"|"4xx5xx"|"error"|"inflight"} [status]
 * @property {boolean} [streamed]  only streamed calls
 * @property {boolean} [hideNoProject]  hide calls with no project
 */

const statusClass = {
  "2xx": (e) => e.finished && e.status >= 200 && e.status < 300,
  "4xx5xx": (e) => e.finished && e.status >= 400,
  error: (e) => e.finished && (e.error !== "" || e.status === 0),
  inflight: isInFlight,
};

/** @param {CallEntry} e @param {LiveFilter} f */
export function entryMatches(e, f) {
  if (f.session && e.session?.id !== f.session) return false;
  if (f.task && e.task?.id !== f.task) return false;
  if (f.model && e.model !== f.model) return false;
  if (f.status && !statusClass[f.status]?.(e)) return false;
  if (f.streamed && !e.streamed) return false;
  if (f.hideNoProject && e.projectId === "") return false;
  const q = (f.query ?? "").trim().toLowerCase();
  if (q) {
    const found = [];
    walk(e.json, [], q, found, true);
    if (!found.length) return false;
  }
  return true;
}

/**
 * The items to show: entries that pass the filter, and every missed calls marker.
 * @param {LiveLog} log
 * @param {LiveFilter} filter
 */
export const filterItems = (log, filter) => log.items.filter((i) => i.type === "missed" || entryMatches(i, filter ?? {}));

/**
 * @param {LiveLog} log
 * @param {LiveFilter} filter
 * @returns {{shown: number, total: number, missed: number}}
 */
export function countsOf(log, filter) {
  const entries = entriesOf(log);
  return { shown: entries.filter((e) => entryMatches(e, filter ?? {})).length, total: entries.length, missed: log.missed };
}

// filter options and the tab a search opens

const sessionLabel = (s) => s.title || `Session ${s.id.slice(0, 8)}`;
const taskLabel = (t) => t.title || "Untitled task";

/**
 * What the filter controls offer: the sessions, tasks and models of the
 * entries held, once each, sorted by name.
 * @param {LiveLog} log
 * @returns {{sessions: {value: string, label: string}[], tasks: {value: string, label: string}[], models: string[]}}
 */
export function filterOptions(log) {
  const sessions = new Map();
  const tasks = new Map();
  const models = new Set();
  for (const e of entriesOf(log)) {
    if (e.session) sessions.set(e.session.id, sessionLabel(e.session));
    if (e.task) tasks.set(e.task.id, taskLabel(e.task));
    if (e.model) models.add(e.model);
  }
  const named = (m) => [...m].map(([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label));
  return { sessions: named(sessions), tasks: named(tasks), models: [...models].sort() };
}

const TAB_OF_ROOT = { request: "request", response: "response", usage: "usage" };

/**
 * The tab a search opens an entry to: the first of Request, Response and Usage
 * whose JSON holds a match, or "" when the matches are only in the entry's
 * other fields (or there is none).
 * @param {CallEntry} entry
 * @param {string} query
 * @returns {""|"request"|"response"|"usage"}
 */
export function matchTab(entry, query) {
  const roots = new Set(searchMatches(entry, query).map((m) => m.path[0]));
  return ["request", "response", "usage"].find((t) => roots.has(TAB_OF_ROOT[t])) ?? "";
}

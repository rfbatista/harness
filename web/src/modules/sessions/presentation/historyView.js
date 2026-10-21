// What the History tab shows: a session's transcript as a chat. A person's
// messages are bubbles, the agent's are rendered Markdown, the tool calls
// between two messages fold into one activity card, system notes are small
// pills, and a divider marks each new day.
//
// Text is bound with x-text. Only an entry's html is placed as HTML: the
// server rendered it from Markdown in safe mode (contract T7), so raw HTML
// and script schemes never reach it.

/**
 * One transcript entry, built once (entries never change once written).
 *
 * @typedef {object} HistoryEntry
 * @property {string} cursor
 * @property {"user" | "assistant" | "tool_use" | "tool_result" | "system"} kind
 * @property {string} time   HH:MM, "" without a time
 * @property {string} stamp  the full time, for the tooltip
 * @property {string} day    the calendar day, "" without a time
 * @property {string} text   as written
 * @property {string} html   the server's rendered Markdown, "" for tools and notes
 * @property {string} tool
 * @property {boolean} isError
 * @property {boolean} cut   the server cut the text at 16 KiB
 * @property {string} lower  text in lower case, for search
 */

/**
 * @typedef {object} TurnPart  one entry of a turn
 * @property {string} cursor
 * @property {string} html
 * @property {string} text
 * @property {boolean} cut
 * @property {boolean} long  folded until shown in full
 *
 * @typedef {object} Call  a tool call and its result
 * @property {string} key     the call's cursor (its result's, without a call)
 * @property {string[]} cursors  the entries it shows
 * @property {string} cursorList  cursors, space-separated, for [data-cursors~=…]
 * @property {string} tool
 * @property {string} gist    the call's one line
 * @property {string} input   the call's summary, "" without a call
 * @property {string} output  the result's text, "" without one
 * @property {boolean} hasOutput
 * @property {"ok" | "error" | "running" | "none"} state
 * @property {string} mark    ✓ ✕ … ·
 * @property {string} meta    "12 lines"
 * @property {boolean} cut
 *
 * @typedef {object} ChatItem
 * @property {string} key
 * @property {"day" | "turn" | "activity" | "failed" | "system"} type
 * @property {string} cursor  the first entry it shows (the scroll anchor)
 * @property {string} [label]       day: "Today"; system: its text
 * @property {"person" | "agent"} [who]
 * @property {string} [name]        who spoke
 * @property {string} [initial]
 * @property {string} [time]
 * @property {string} [stamp]
 * @property {TurnPart[]} [parts]
 * @property {Call[]} [calls]
 * @property {string} [summary]     "3 tool calls"
 * @property {string} [names]       "Bash ×2, Read"
 * @property {number} [failed]      calls that failed
 * @property {string} [failedLabel] "1 failed"
 * @property {boolean} [running]    a call waits for its result
 */

/** An agent message longer than this folds until shown in full. */
export const LONG_TEXT = 2_000;

const two = (n) => String(n).padStart(2, "0");
const dayOf = (d) => `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`;

/** @param {import("../domain/transcript.js").TranscriptEntry} entry @returns {HistoryEntry} */
export function historyEntry(entry) {
  const at = entry.at;
  return {
    cursor: entry.cursor,
    kind: entry.kind,
    time: at ? `${two(at.getHours())}:${two(at.getMinutes())}` : "",
    stamp: at ? at.toLocaleString() : "",
    day: at ? dayOf(at) : "",
    text: entry.text,
    html: entry.html ?? "",
    tool: entry.tool,
    isError: entry.isError,
    cut: entry.truncated,
    lower: entry.text.toLowerCase(),
  };
}

/** "Today", "Yesterday", or the date. */
export function dayLabel(day, now = new Date()) {
  if (day === dayOf(now)) return "Today";
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (day === dayOf(yesterday)) return "Yesterday";
  const [y, m, d] = day.split("-").map(Number);
  const date = new Date(y, m - 1, d);
  return date.toLocaleDateString(undefined, { weekday: "long", day: "numeric", month: "short", ...(y === now.getFullYear() ? {} : { year: "numeric" }) });
}

const firstLine = (text) => text.split("\n").find((l) => l.trim() !== "")?.trim() ?? "";

function callOf(e) {
  return { key: e.cursor, cursors: [e.cursor], tool: e.tool, gist: firstLine(e.text), input: e.text, output: "", hasOutput: false, state: "none", mark: "·", meta: "", cut: e.cut };
}

/** A result fills the open call of its tool, oldest first; without one it is its own row. */
function addResult(calls, e) {
  let call = calls.find((c) => !c.hasOutput && c.tool === e.tool);
  if (!call) {
    call = { ...callOf(e), gist: firstLine(e.text) || "no output", input: "" };
    calls.push(call);
  } else {
    call.cursors.push(e.cursor);
  }
  call.output = e.text;
  call.hasOutput = true;
  call.cut ||= e.cut;
  const lines = e.text === "" ? 0 : e.text.split("\n").length;
  call.meta = lines > 1 ? `${lines} lines` : "";
}

function finish(activity, running) {
  const counts = new Map();
  let failed = 0;
  for (const c of activity.calls) {
    counts.set(c.tool, (counts.get(c.tool) ?? 0) + 1);
    c.state = c.hasOutput ? (activity.isError.has(c) ? "error" : "ok") : running ? "running" : "none";
    c.mark = { ok: "✓", error: "✕", running: "…", none: "·" }[c.state];
    if (c.state === "error") failed++;
    c.cursorList = c.cursors.join(" ");
  }
  const n = activity.calls.length;
  activity.summary = `${n} tool call${n === 1 ? "" : "s"}`;
  activity.names = [...counts].map(([t, k]) => (k > 1 ? `${t} ×${k}` : t)).join(", ");
  activity.failed = failed;
  activity.failedLabel = `${failed} failed`;
  activity.running = activity.calls.some((c) => c.state === "running");
  delete activity.isError;
}

/**
 * The chat for a run of entries, oldest first.
 *
 * @param {HistoryEntry[]} entries
 * @param {{ agent?: string, live?: boolean, showTools?: boolean, now?: Date }} [o]
 *   live: the session runs, so a call without its result is still running
 * @returns {ChatItem[]}
 */
export function chatItems(entries, { agent = "Agent", live = false, showTools = true, now = new Date() } = {}) {
  /** @type {ChatItem[]} */
  const items = [];
  let day = "";
  let activity = null;
  let lastActivity = null;
  const close = () => {
    activity = null;
  };
  for (const e of entries) {
    if (e.day && e.day !== day) {
      close();
      day = e.day;
      items.push({ key: `day:${e.day}:${e.cursor}`, type: "day", cursor: "", label: dayLabel(e.day, now) });
    }
    if (e.kind === "tool_use" || e.kind === "tool_result") {
      if (!activity) {
        activity = { key: `act:${e.cursor}`, type: "activity", cursor: e.cursor, calls: [], isError: new Set() };
        items.push(activity);
        lastActivity = activity;
      }
      if (e.kind === "tool_use") activity.calls.push(callOf(e));
      else {
        addResult(activity.calls, e);
        if (e.isError) activity.isError.add(activity.calls.find((c) => c.cursors.includes(e.cursor)));
      }
      continue;
    }
    close();
    if (e.kind === "system") {
      items.push({ key: e.cursor, type: "system", cursor: e.cursor, label: e.text, time: e.time, stamp: e.stamp });
      continue;
    }
    const who = e.kind === "user" ? "person" : "agent";
    const part = { cursor: e.cursor, html: e.html, text: e.text, cut: e.cut, long: who === "agent" && e.text.length > LONG_TEXT };
    const last = items.at(-1);
    if (who === "agent" && last?.type === "turn" && last.who === "agent") {
      last.parts.push(part);
      continue;
    }
    const name = who === "person" ? "You" : agent;
    items.push({ key: e.cursor, type: "turn", cursor: e.cursor, who, name, initial: name.charAt(0).toUpperCase(), time: e.time, stamp: e.stamp, parts: [part] });
  }
  for (const it of items) if (it.type === "activity") finish(it, live && it === lastActivity);
  if (showTools) return items;
  // Tools hidden: only the conversation, but a failed call still shows.
  return items
    .filter((it) => it.type !== "activity" || it.failed > 0)
    .map((it) => (it.type === "activity" ? { ...it, key: `failed:${it.key}`, type: "failed", summary: `${it.failed} tool call${it.failed === 1 ? "" : "s"} failed` } : it));
}

/** The cursors of the entries whose text holds query (any case), oldest first. */
export function matches(entries, query, showTools = true) {
  const q = query.trim().toLowerCase();
  if (q === "") return [];
  return entries.filter((e) => (showTools || (e.kind !== "tool_use" && e.kind !== "tool_result")) && e.lower.includes(q)).map((e) => e.cursor);
}

/** The agent is between its turns: the newest entry is not its answer. */
export function isWorking(entries, live) {
  if (!live || entries.length === 0) return false;
  const kind = entries.at(-1).kind;
  return kind === "user" || kind === "tool_use" || kind === "tool_result";
}

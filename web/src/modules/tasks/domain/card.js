// How a task card reads its signals: the time in its column, the last move,
// and the merge badge. The BFF draws the first paint by the same rules
// (internal/adapter/in/web/tasks/board.go); web/testdata/views/task-card.json
// pins them together.

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** How long a task may sit in a column before its age says "stale"; columns not listed never go stale. */
export const STALE_AFTER = Object.freeze({ in_progress: 3 * DAY, review: 2 * DAY });

/** The statuses whose cards carry the merge badge. */
const MERGE_STATUSES = new Set(["review", "done"]);

/**
 * The time a task has been in its column: "now", "4m", "2h", "yesterday",
 * "5d", then the date from 30 days on; " · stale" past its column's threshold.
 * since is null when the time is unknown, and then text is "".
 * @param {import("./task.js").Task} task
 * @param {Date} now
 * @returns {{ text: string, stale: boolean, since: number | null }}
 */
export function age(task, now) {
  const since = task.statusSince || 0;
  if (!since) return { text: "", stale: false, since: null };
  const ms = Math.max(0, now.getTime() - since);
  const stale = ms > (STALE_AFTER[task.status] ?? Infinity);
  const text = elapsed(ms, since, now);
  return { text: stale ? `${text} · stale` : text, stale, since };
}

function elapsed(ms, since, now) {
  if (ms < MINUTE) return "now";
  if (ms < HOUR) return `${Math.floor(ms / MINUTE)}m`;
  if (ms < DAY) return `${Math.floor(ms / HOUR)}h`;
  if (ms < 2 * DAY) return "yesterday";
  if (ms < 30 * DAY) return `${Math.floor(ms / DAY)}d`;
  const d = new Date(since);
  const day = `${MONTHS[d.getMonth()]} ${d.getDate()}`;
  return d.getFullYear() === now.getFullYear() ? day : `${day}, ${d.getFullYear()}`;
}

/** "Oct 19, 09:12": a moment to the minute, in local time. */
export function exactTime(ms) {
  const d = new Date(ms);
  const pad = (n) => String(n).padStart(2, "0");
  return `${MONTHS[d.getMonth()]} ${d.getDate()}, ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/**
 * Who put the task in its column: "Moved here by" you, the architect or the
 * session's label; "Created by you" when it never moved.
 * @param {import("./task.js").Task} task
 * @returns {{ lead: string, who: string, reason: string }}
 */
export function lastMoveLine(task) {
  const m = task.lastMove;
  if (!m) return { lead: "Created by", who: "you", reason: "" };
  const who = m.by === "person" ? "you" : m.by === "architect" ? "architect" : m.sessionLabel || "a session";
  return { lead: "Moved here by", who, reason: m.reason ?? "" };
}

const MERGE_WORDS = {
  merged: () => ({ word: "Merged", rail: "merged" }),
  partial: ({ merged, total }) => ({ word: `${merged} of ${total} merged`, rail: `${merged}/${total} merged` }),
  not_merged: () => ({ word: "Not merged", rail: "not merged" }),
  unknown: () => ({ word: "Unknown", rail: "unknown" }),
};

/**
 * The merge badge of a Review or Done card: its state and word, and the
 * rail's lower-case word. null when there is nothing to show (none, missing,
 * or a state this client does not know).
 * @param {import("./task.js").Task} task
 * @returns {{ state: string, word: string, rail: string } | null}
 */
export function mergeBadge(task) {
  const m = task.mergeState;
  if (!m || !MERGE_STATUSES.has(task.status) || !Object.hasOwn(MERGE_WORDS, m.state)) return null;
  return { state: m.state, ...MERGE_WORDS[m.state](m) };
}

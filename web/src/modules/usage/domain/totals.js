// A task's and its sessions' all-time token totals, as the task page shows
// them: seeded by the server, then grown by each live usage record
// attributed to that task or to one of those sessions.

import { addToTotals, emptyTotals } from "./usage.js";

/** How many record ids are remembered to ignore a record seen twice. */
const SEEN_CAP = 500;

/**
 * taskId is "" on a page without a task; task is null then, or when its
 * totals were not read.
 * @typedef {{ taskId: string, task: import("./usage.js").Totals|null,
 *             sessions: Record<string, import("./usage.js").Totals>, seen: string[] }} LiveTotals
 */

/**
 * @param {{ taskId: string, task: import("./usage.js").Totals|null, sessions: Record<string, import("./usage.js").Totals> }} seed
 * @returns {LiveTotals}
 */
export function startTotals(seed) {
  return { taskId: seed.taskId, task: seed.task, sessions: { ...seed.sessions }, seen: [] };
}

/**
 * The totals with r counted when it belongs: to the task (and its session,
 * new ones included), or to a session the page shows. Anything else, or a
 * record already counted, gives back the same totals.
 * @param {LiveTotals} t @param {import("./usage.js").UsageRecord} r @returns {LiveTotals}
 */
export function addLiveRecord(t, r) {
  if (t.seen.includes(r.id)) return t;
  const ofTask = t.taskId !== "" && r.taskId === t.taskId;
  const ofSession = r.sessionId !== "" && (ofTask || r.sessionId in t.sessions);
  if (!ofTask && !ofSession) return t;
  return {
    taskId: t.taskId,
    task: ofTask ? addToTotals(t.task ?? emptyTotals(), r) : t.task,
    sessions: ofSession ? { ...t.sessions, [r.sessionId]: addToTotals(t.sessions[r.sessionId] ?? emptyTotals(), r) } : t.sessions,
    seen: [...t.seen, r.id].slice(-SEEN_CAP),
  };
}

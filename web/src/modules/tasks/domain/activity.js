// What the rail shows per task: how many of its sessions are live and whether
// one waits on the developer. The rules are the BFF's sessions.IsLive and
// sessions.NeedsYou (internal/adapter/in/web/sessions/view.go).

/**
 * @typedef {object} RailSession  a session as the rail needs it
 * @property {string} id
 * @property {string} ticketId
 * @property {string} status
 * @property {number} pendingApprovals
 *
 * @typedef {{ live: number, attention: boolean }} Activity
 */

const ENDED = new Set(["done", "stopped", "failed"]);

export const isLive = (s) => !ENDED.has(s.status);

export const needsYou = (s) => isLive(s) && (s.status === "waiting_approval" || s.status === "idle" || s.pendingApprovals > 0);

/** @returns {Record<string, Activity>} by task id; sessions without a task are left out */
export function activityByTask(sessions) {
  const out = {};
  for (const s of sessions) {
    if (!s.ticketId) continue;
    const a = (out[s.ticketId] ??= { live: 0, attention: false });
    if (isLive(s)) a.live++;
    a.attention ||= needsYou(s);
  }
  return out;
}

/**
 * A rail link's dot: the BFF's Link.liveState / liveWord. Reviews the
 * architect raised wait on the person as much as a session's prompt does.
 */
export function linkState(activity, pendingReviews = 0) {
  if (activity.attention || pendingReviews > 0) return { state: "waiting", word: "waiting on you" };
  if (activity.live > 0) return { state: "running", word: "running" };
  return { state: "", word: "" };
}

/** Applies one feed change to the rail's sessions. */
export function applyRailChange(sessions, change) {
  const rest = sessions.filter((s) => s.id !== (change.kind === "deleted" ? change.id : change.session.id));
  return change.kind === "deleted" ? rest : [...rest, change.session];
}

// Rule events as the overview lists them: a rule of the architecture that
// failed in a session, newest first. Pure.


/** How many events the overview keeps. */
export const EVENTS_SHOWN = 50;

/**
 * @param {any} ev  An event of `list_architecture_rule_events`.
 * @param {string} projectId
 * @param {Date} now
 */
export function ruleEventRow(ev, projectId, now) {
  const blocked = ev.outcome === "blocked";
  const when = new Date(ev.at);
  const ago = Number.isNaN(when.getTime()) ? "" : compact(now.getTime() - when.getTime());
  return {
    id: ev.id,
    agent: ev.agent || "plain",
    rule: ev.rule_id,
    word: blocked ? "Blocked" : "Warned",
    glyph: blocked ? "✕" : "!",
    tone: blocked ? "danger" : "attention",
    path: ev.path || "",
    layer: ev.layer || "no layer",
    when: ago === "now" ? "now" : ago ? `${ago} ago` : "",
    sessionHref: ev.session_id && ev.task_id
      ? `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(ev.task_id)}?session=${encodeURIComponent(ev.session_id)}`
      : "",
  };
}

/** The list with `ev` at the top: an event already there is not repeated, and the list is capped. */
export function prependEvent(events, ev) {
  if (events.some((e) => e.id === ev.id)) return events;
  return [ev, ...events].slice(0, EVENTS_SHOWN);
}

/** "now", "4m", "3h", "2d": how long ago, compactly. */
function compact(ms) {
  const minutes = Math.floor(Math.max(0, ms) / 60_000);
  if (minutes < 1) return "now";
  if (minutes < 60) return `${minutes}m`;
  if (minutes < 24 * 60) return `${Math.floor(minutes / 60)}h`;
  return `${Math.floor(minutes / (24 * 60))}d`;
}

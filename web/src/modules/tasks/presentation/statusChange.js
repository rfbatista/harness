// A task's latest status move as its page says it: who moved it, when, and
// why. Live only: the move arrives over the feed (task_status); a reload
// starts without it.

import { relativeTime } from "../../../shared/presentation/format.js";
import { STATUSES } from "../domain/task.js";

/**
 * @param {import("../domain/board.js").StatusChange} change
 * @param {{ architectSessionId: string, now: Date }} ctx
 * @returns {{ line: string, reason: string }}  reason quoted, or ""
 */
export function statusChangeView(change, { architectSessionId, now }) {
  const status = STATUSES.find((s) => s.value === change.status)?.label.toLowerCase() ?? change.status.replaceAll("_", " ");
  const by =
    change.by === "person" ? "a person" : change.bySessionId && change.bySessionId === architectSessionId ? "the architect" : "a session";
  const when = change.at ? ` · ${relativeTime(change.at, now)}` : "";
  return { line: `Moved to ${status} by ${by}${when}`, reason: change.reason ? `“${change.reason}”` : "" };
}

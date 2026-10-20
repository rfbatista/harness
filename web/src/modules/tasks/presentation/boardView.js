// The board as the markup binds it: columns in board order, newest move on
// top, each card with its signals (live, reviews, merge), its last move, its
// time in column and the link to its task.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { byStatus, moveChoices } from "../domain/board.js";
import { age, exactTime, lastMoveLine, mergeBadge } from "../domain/card.js";
import { STATUSES } from "../domain/task.js";

const QUIET = Object.freeze({ live: 0, attention: false });

/** What an empty column says. */
const EMPTY = Object.freeze({
  review: "Nothing waiting for your check.",
  done: "No done tasks. Drag a checked task here from Review.",
});

const statusLabel = (status) => STATUSES.find((s) => s.value === status)?.label ?? status;

/** A task's page. */
export const taskHref = (projectId, taskId) => `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;

/**
 * @param {import("../domain/task.js").Task[]} tasks
 * @param {{
 *   projectId: string,
 *   byTask: Record<string, import("../domain/activity.js").Activity>,
 *   fresh: Set<string>,
 *   now: Date,
 *   pending?: Record<string, string>,  per card with a move in flight, the status it goes to
 *   failed?: Set<string>,              cards that went back after a refused move
 * }} ctx
 */
export function toColumns(tasks, { projectId, byTask, fresh, now, pending = {}, failed = new Set() }) {
  const cols = byStatus(tasks);
  return STATUSES.map(({ value, label }) => {
    const cards = cols[value].map((t) => toCard(t, { projectId, activity: byTask[t.id] ?? QUIET, fresh: fresh.has(t.id), now, pendingTo: pending[t.id] ?? "", failed: failed.has(t.id) }));
    const waiting = value === "review" && cards.length > 0;
    return {
      status: value,
      label,
      count: cards.length,
      isEmpty: cards.length === 0,
      emptyText: EMPTY[value] ?? "No tasks",
      waiting,
      sub: waiting ? "Waiting for your check" : "",
      cards,
    };
  });
}

function toCard(task, { projectId, activity, fresh, now, pendingTo, failed }) {
  // The reviews badge says the reviews; the dot is about sessions only.
  const { state } = linkState(activity);
  const merge = mergeBadge(task);
  const move = lastMoveLine(task);
  const time = age(task, now);
  const since = time.since ? exactTime(time.since) : "";
  return {
    id: task.id,
    title: task.title,
    href: taskHref(projectId, task.id),
    status: task.status,
    fresh,
    liveState: state,
    liveWord: state === "waiting" ? "waiting" : state === "running" ? `${activity.live} live` : "",
    hasLive: state !== "",
    reviews: task.pendingReviews > 0 ? count(task.pendingReviews, "review") : "",
    hasReviews: task.pendingReviews > 0,
    hasMerge: merge !== null,
    mergeState: merge?.state ?? "",
    mergeWord: merge?.word ?? "",
    hasSignals: state !== "" || task.pendingReviews > 0 || merge !== null,
    lead: move.lead,
    who: move.who,
    reason: move.reason,
    hasReason: move.reason !== "",
    at: since ? ` · ${since}` : "",
    age: time.text,
    stale: time.stale,
    ageTitle: since ? `In this column since ${since}` : "",
    pending: pendingTo !== "",
    failed,
    note: pendingTo ? `Moving to ${statusLabel(pendingTo)}…` : failed ? "Move failed" : "",
    choices: moveChoices(task.status),
    canArchive: task.status === "done",
  };
}

/**
 * "Archive all" on the Done header and its in-page confirmation strip, as
 * the markup binds them. phase: rest (the button), confirm, busy (one
 * request, not optimistic), done (the column says how many went), failed.
 * The strip's count follows Done while it is open; Done emptying closes it.
 * @param {{ phase: "rest"|"confirm"|"busy"|"done"|"failed", done: number, archived?: number }} state
 */
export function archiveAllView({ phase, done, archived = 0 }) {
  if (phase === "confirm" && done === 0) phase = "rest";
  const tasks = (n) => `${n} done ${n === 1 ? "task" : "tasks"}`;
  const view = { shows: done > 0 && (phase === "rest" || phase === "done"), open: false, phase, title: "", body: "", confirm: "", emptyText: "", asking: false, busy: false, failed: false, tone: null };
  switch (phase) {
    case "confirm":
      return { ...view, open: true, asking: true, title: `Archive all ${tasks(done)}?`, body: "They leave the board and the rail. Nothing is lost: restore any of them from Archived.", confirm: `Archive ${done}` };
    case "busy":
      return { ...view, open: true, asking: true, busy: true, title: `Archiving ${done}…`, confirm: `Archive ${done}` };
    case "failed":
      return { ...view, open: true, failed: true, tone: "danger", title: "Couldn't archive. Nothing changed." };
    case "done":
      return { ...view, emptyText: done === 0 ? `${archived} ${archived === 1 ? "task" : "tasks"} archived.` : "" };
  }
  return view;
}

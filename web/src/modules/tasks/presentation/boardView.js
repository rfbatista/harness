// The board as the markup binds it: columns in board order, newest move on
// top, each card with its signals (live, reviews, merge), its last move, its
// time in column and the link to its task.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { byStatus, moveChoices } from "../domain/board.js";
import { age, exactTime, lastMoveLine, mergeBadge } from "../domain/card.js";
import { childrenOf, childrenWord, parentOf } from "../domain/feature.js";
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
 *   drag?: { id: string, from: string, over: string } | null,  the card being dragged, its column, and the column under the pointer ("" for none)
 *   fine?: boolean,                    a fine pointer: cards drag (touch uses the menu)
 * }} ctx
 */
export function toColumns(tasks, { projectId, byTask, fresh, now, pending = {}, failed = new Set(), drag = null, fine = false }) {
  const cols = byStatus(tasks);
  return STATUSES.map(({ value, label }) => {
    const cards = cols[value].map((t) =>
      toCard(t, { projectId, activity: byTask[t.id] ?? QUIET, fresh: fresh.has(t.id), now, pendingTo: pending[t.id] ?? "", failed: failed.has(t.id), ghost: drag?.id === t.id, fine, all: tasks }),
    );
    const waiting = value === "review" && cards.length > 0;
    const under = Boolean(drag) && drag.over === value;
    const dropOver = under && drag.from !== value;
    return {
      status: value,
      anchor: columnAnchor(value),
      label,
      count: cards.length,
      countText: dropOver ? `${cards.length} → ${cards.length + 1}` : String(cards.length),
      dragOrigin: Boolean(drag) && drag.from === value,
      dropOver,
      dropSelf: under && drag.from === value,
      dropText: `Drop to move to ${label}`,
      selfText: `Already in ${label}`,
      isEmpty: cards.length === 0,
      emptyText: EMPTY[value] ?? "No tasks",
      waiting,
      sub: waiting ? "Waiting for your check" : "",
      cards,
    };
  });
}

/** all: every task of the project, for the feature cues (the parent's title, the children count). */
function toCard(task, { projectId, activity, fresh, now, pendingTo, failed, ghost = false, fine = false, all = [] }) {
  // The reviews badge says the reviews; the dot is about sessions only.
  const { state } = linkState(activity);
  const merge = mergeBadge(task);
  const move = lastMoveLine(task);
  const time = age(task, now);
  const since = time.since ? exactTime(time.since) : "";
  const parentTitle = parentOf(all, task)?.title ?? "";
  const children = childrenWord(childrenOf(all, task).length);
  return {
    id: task.id,
    title: task.title,
    parentTitle,
    hasParent: parentTitle !== "",
    children,
    hasChildren: children !== "",
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
    hasSignals: children !== "" || state !== "" || task.pendingReviews > 0 || merge !== null,
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
    ghost,
    draggable: fine && pendingTo === "",
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

/** A column's anchor: the jump chips link to it. */
export const columnAnchor = (status) => `column-${status}`;

/**
 * The narrow screen's jump chips (below 48rem): one anchor link per column,
 * with its label and count, never tabs, so every column stays in the
 * scroller. Review's chip asks for a look while it holds cards; bumped is the
 * column a menu move just filled, whose chip rings for a moment.
 * @param {{ status: string, label: string, count: number, waiting: boolean }[]} columns
 * @param {{ bumped: string }} ctx
 */
export function jumpChips(columns, { bumped }) {
  return columns.map((c) => ({ status: c.status, href: `#${columnAnchor(c.status)}`, label: c.label, count: c.count, waiting: c.waiting, bumped: c.status === bumped }));
}

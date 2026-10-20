// The board: the project's tasks by status, and how a change to one of them
// reads. The rail shows the same tasks in its own order (RAIL_ORDER, the
// BFF's `kanban` in internal/adapter/in/web/tasks/rail.go).

import { STATUSES } from "./task.js";

/**
 * @typedef {import("./task.js").Task} Task
 * @typedef {{ kind: "task-upsert", task: Task } | { kind: "task-deleted", id: string }} TaskChange
 */

/** The rail's group order: what is moving first, done last. A permutation of STATUSES. */
export const RAIL_ORDER = Object.freeze(["in_progress", "review", "todo", "backlog", "done"]);

/** Applies one feed change. A changed task keeps its place in the list, so a move does not shuffle its column. */
export function applyTaskChange(tasks, change) {
  if (change.kind === "task-deleted") return tasks.filter((t) => t.id !== change.id);
  const at = tasks.findIndex((t) => t.id === change.task.id);
  if (at < 0) return [...tasks, change.task];
  return tasks.map((t, i) => (i === at ? change.task : t));
}

/**
 * Every status, in STATUSES order; in each, the task that entered it last on
 * top (status_since, newest first), as the server renders the board. Ties keep
 * list order; a task whose time is unknown goes last.
 * @returns {Record<string, Task[]>}
 */
export function byStatus(tasks) {
  const out = Object.fromEntries(STATUSES.map((s) => [s.value, []]));
  for (const t of tasks) out[t.status]?.push(t);
  for (const list of Object.values(out)) list.sort((a, b) => (b.statusSince || 0) - (a.statusSince || 0));
  return out;
}

/**
 * The "Move to…" menu's statuses for a task in `status`, in board order: each
 * with its key (1 to 5), the current one marked. initial is where the menu
 * opens: the status after the current one, the most likely next step; from
 * Done it is Review, so an accidental Enter stays reversible.
 * @returns {{ status: string, label: string, key: string, current: boolean, initial: boolean }[]}
 */
export function moveChoices(status) {
  const at = STATUSES.findIndex((s) => s.value === status);
  const next = at === STATUSES.length - 1 ? at - 1 : at + 1;
  return STATUSES.map((s, i) => ({ status: s.value, label: s.label, key: String(i + 1), current: i === at, initial: i === next }));
}

const label = (status) => STATUSES.find((s) => s.value === status)?.label ?? status;

/** How a change reads to a person; "" when nothing worth saying changed. */
export function describeChange(previous, change) {
  if (change.kind === "task-deleted") return previous ? `${previous.title} was removed` : "";
  const t = change.task;
  if (!previous) return `${t.title} was added to ${label(t.status)}`;
  if (previous.status !== t.status) return `${previous.title} moved to ${label(t.status)}`;
  if (previous.title !== t.title) return `${previous.title} was renamed to ${t.title}`;
  return "";
}

/**
 * Who moved a task's status, and why: the project feed's `task_status`.
 * at is when it happened; null when the server did not say.
 * @typedef {{ taskId: string, status: string, reason: string, by: "session"|"person", bySessionId: string, at: Date | null }} StatusChange
 * @typedef {{ kind: "task-status", change: StatusChange }} StatusChangeEvent
 */

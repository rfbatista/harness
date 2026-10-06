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

/** @returns {Record<string, Task[]>} every status, in STATUSES order, tasks in list order */
export function byStatus(tasks) {
  const out = Object.fromEntries(STATUSES.map((s) => [s.value, []]));
  for (const t of tasks) out[t.status]?.push(t);
  return out;
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

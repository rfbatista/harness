// A task as the rail reads it: the ticket of internal/domain/ticket.go, as much
// as the rail draws. Times are ms since the epoch, 0 when unknown.

export const STATUSES = [
  { value: "backlog", label: "Backlog" },
  { value: "todo", label: "Todo" },
  { value: "in_progress", label: "In progress" },
  { value: "review", label: "Review" },
  { value: "done", label: "Done" },
] as const;

export type TaskStatus = (typeof STATUSES)[number]["value"];

export const TASK_STATUSES: readonly TaskStatus[] = STATUSES.map((s) => s.value);

/** The rail's group order: what is moving first, done last. */
export const RAIL_ORDER: readonly TaskStatus[] = ["in_progress", "review", "todo", "backlog", "done"];

export type MergeStateKind = "merged" | "partial" | "not_merged" | "unknown" | "none";

/** Whether a task's branches landed; Review and Done only. */
export interface MergeState {
  readonly state: MergeStateKind | string;
  readonly merged: number;
  readonly total: number;
}

export interface Task {
  readonly id: string;
  readonly projectId: string;
  readonly title: string;
  readonly status: TaskStatus;
  /** The feature task it belongs to; "" when top-level (one level deep). */
  readonly parentId: string;
  /** Review requests the architect raised that wait on the person. */
  readonly pendingReviews: number;
  readonly updatedAt: number;
  /** When it entered its status; the order of a feature's children. */
  readonly statusSince: number;
  /** When it was archived; 0 when it is not. */
  readonly archivedAt: number;
  readonly mergeState: MergeState | null;
}

/** One change of the project feed that moves the task list. */
export type TaskChange = { readonly kind: "task-upsert"; readonly task: Task } | { readonly kind: "task-deleted"; readonly id: string };

/**
 * The tasks the rail may draw: every one not archived, and a child archived on
 * its own while its feature is on the board (the feature's row lists it).
 */
export const onBoard = (tasks: readonly Task[]): Task[] => tasks.filter((t) => !t.archivedAt || (t.parentId !== "" && tasks.some((p) => p.id === t.parentId && !p.archivedAt)));

/**
 * Applies one feed change. A changed task keeps its place in the list. An
 * archived task leaves the list with the children archived beside it, unless it
 * is a child archived on its own while its feature is on the board. A restored
 * one (archived_at cleared) comes back.
 */
export function applyTaskChange(tasks: readonly Task[], change: TaskChange): Task[] {
  const archivedAlone = change.kind === "task-upsert" && change.task.archivedAt !== 0 && tasks.some((p) => p.id === change.task.parentId && !p.archivedAt);
  if (change.kind === "task-deleted" || (change.task.archivedAt && !archivedAlone)) {
    const id = change.kind === "task-deleted" ? change.id : change.task.id;
    return tasks.filter((t) => t.id !== id && !(t.archivedAt && t.parentId === id));
  }
  const at = tasks.findIndex((t) => t.id === change.task.id);
  if (at < 0) return [...tasks, change.task];
  return tasks.map((t, i) => (i === at ? change.task : t));
}

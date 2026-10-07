// The board as the markup binds it: columns in board order, each card with
// the rail's dot and count and the link to its task.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { byStatus } from "../domain/board.js";
import { STATUSES } from "../domain/task.js";

const QUIET = Object.freeze({ live: 0, attention: false });

/** A task's page. */
export const taskHref = (projectId, taskId) => `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;

/**
 * @param {import("../domain/task.js").Task[]} tasks
 * @param {{ projectId: string, byTask: Record<string, import("../domain/activity.js").Activity>, fresh: Set<string> }} ctx
 */
export function toColumns(tasks, { projectId, byTask, fresh }) {
  const cols = byStatus(tasks);
  return STATUSES.map(({ value, label }) => {
    const cards = cols[value].map((t) => toCard(t, projectId, byTask[t.id] ?? QUIET, fresh.has(t.id)));
    return { status: value, label, count: cards.length, isEmpty: cards.length === 0, cards };
  });
}

function toCard(task, projectId, activity, fresh) {
  const { state, word } = linkState(activity, task.pendingReviews);
  return {
    id: task.id,
    title: task.title,
    href: taskHref(projectId, task.id),
    status: task.status,
    state,
    word,
    hasDot: state !== "",
    live: activity.live,
    hasCount: activity.live > 0,
    fresh,
    moveLabel: `Move ${task.title} to`,
    reviews: task.pendingReviews > 0 ? count(task.pendingReviews, "review") : "",
    hasReviews: task.pendingReviews > 0,
  };
}

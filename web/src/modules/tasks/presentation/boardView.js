// The board as the markup binds it: columns in board order, each card with
// the rail's dot and count and the link to its task.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { byStatus } from "../domain/board.js";
import { childrenOf, childrenWord, parentOf } from "../domain/feature.js";
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
    const cards = cols[value].map((t) => toCard(t, projectId, byTask[t.id] ?? QUIET, fresh.has(t.id), tasks));
    return { status: value, label, count: cards.length, isEmpty: cards.length === 0, cards };
  });
}

/** all: every task of the project, for the feature cues (the parent's title, the children count). */
function toCard(task, projectId, activity, fresh, all = []) {
  const { state, word } = linkState(activity, task.pendingReviews);
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

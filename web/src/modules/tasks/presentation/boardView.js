// The board as the markup binds it: columns in board order, newest move on
// top, each card with its signals (live, reviews, merge), its last move, its
// time in column and the link to its task.

import { count } from "../../../shared/presentation/format.js";
import { linkState } from "../domain/activity.js";
import { byStatus } from "../domain/board.js";
import { age, exactTime, lastMoveLine, mergeBadge } from "../domain/card.js";
import { STATUSES } from "../domain/task.js";

const QUIET = Object.freeze({ live: 0, attention: false });

/** What an empty column says. */
const EMPTY = Object.freeze({
  review: "Nothing waiting for your check.",
  done: "No done tasks. Drag a checked task here from Review.",
});

/** A task's page. */
export const taskHref = (projectId, taskId) => `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;

/**
 * @param {import("../domain/task.js").Task[]} tasks
 * @param {{ projectId: string, byTask: Record<string, import("../domain/activity.js").Activity>, fresh: Set<string>, now: Date }} ctx
 */
export function toColumns(tasks, { projectId, byTask, fresh, now }) {
  const cols = byStatus(tasks);
  return STATUSES.map(({ value, label }) => {
    const cards = cols[value].map((t) => toCard(t, projectId, byTask[t.id] ?? QUIET, fresh.has(t.id), now));
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

function toCard(task, projectId, activity, fresh, now) {
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
    moveLabel: `Move ${task.title} to`,
  };
}

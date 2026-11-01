// Feature tasks: a task may have a parent, one level deep, in the same
// project (ADR 0016). A task with children is a feature task. The server
// keeps the invariant; here only what the board and the task page show is
// derived from the project's task list.

/**
 * @typedef {import("./task.js").Task} Task
 */

/** The task's parent, or null when it has none or the parent is not in the list. */
export function parentOf(tasks, task) {
  if (!task?.parentId) return null;
  return tasks.find((t) => t.id === task.parentId) ?? null;
}

/** The task's children, in list order. */
export function childrenOf(tasks, task) {
  if (!task?.id) return [];
  return tasks.filter((t) => t.parentId === task.id);
}

/** How many children a feature task has, in words: "1 task", "3 tasks"; "" for none. */
export function childrenWord(n) {
  if (!n || n < 1) return "";
  return n === 1 ? "1 task" : `${n} tasks`;
}

// The feature card (ADR 0058): a feature task and its children are one card
// on the board, in the column of the feature's own status. The BFF's
// featureCard (internal/adapter/in/web/tasks/board_feature.go) says the same;
// web/testdata/views/feature-cards.json pins them together.

/** The order a feature card lists its children: what waits on the person first, done last. */
const CHILD_ORDER = ["review", "in_progress", "todo", "backlog", "done"];

/** The progress bar's fill order, left to right, and how each status reads in its text. */
const BAR_ORDER = [
  ["done", "done"],
  ["review", "review"],
  ["in_progress", "in progress"],
  ["todo", "todo"],
  ["backlog", "backlog"],
];

const STATUS_LABELS = { backlog: "Backlog", todo: "Todo", in_progress: "In progress", review: "Review", done: "Done" };

const taskWord = (n) => (n === 1 ? "1 task" : `${n} tasks`);

/** Whether a task is drawn inside its feature's card: not archived itself, or archived alone while its feature is on the board. */
const isLive = (tasks, id) => tasks.some((t) => t.id === id && !t.archivedAt);

/** True when the task has no card of its own on the board: an archived task, or a child whose feature is on the board. */
export const isInsideCard = (tasks, task) => Boolean(task.archivedAt) || (task.parentId !== "" && isLive(tasks, task.parentId));

/** A feature task's children, archived-alone ones included, as its card lists them: Review, In progress, Todo, Backlog, Done; the newest move first within a status. */
export function cardChildren(tasks, task) {
  return childrenOf(tasks, task).sort((a, b) => CHILD_ORDER.indexOf(a.status) - CHILD_ORDER.indexOf(b.status) || (b.statusSince || 0) - (a.statusSince || 0));
}

/** The progress bar, its text equivalent, the summary, the disclosure's label and the status mismatch hint. */
export function featureProgress(task, children) {
  const total = (status) => children.filter((c) => c.status === status).length;
  const segments = BAR_ORDER.filter(([status]) => total(status) > 0).map(([status, word]) => ({ status, word, count: total(status), style: `flex-grow:${total(status)}` }));
  const open = children.length - total("done");
  let hint = "";
  let hintNote = "";
  if (["backlog", "todo", "in_progress"].includes(task.status) && open === 0) {
    hint = `All ${taskWord(children.length)} done.`;
    hintNote = "Move the feature to Review when it is ready.";
  } else if (["review", "done"].includes(task.status) && open > 0) {
    hint = `${taskWord(open)} open.`;
    hintNote = `The feature is in ${STATUS_LABELS[task.status]} while a child is not done.`;
  }
  return {
    segments,
    bar: segments.map((s) => `${s.count} ${s.word}`).join(", "),
    summary: `${total("done")} of ${children.length} done`,
    tasks: taskWord(children.length),
    open,
    hint,
    hintNote,
  };
}

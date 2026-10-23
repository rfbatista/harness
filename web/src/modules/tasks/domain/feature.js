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

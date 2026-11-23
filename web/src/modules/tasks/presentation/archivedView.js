// The Archived view as the markup binds it: the project's archived tasks,
// newest archived first, each with when it was finished and archived, its
// last known merge badge, Restore and Delete; a row that was restored or
// deleted collapses to a one-line receipt until the person leaves the page.

import { mergeBadge } from "../domain/card.js";
import { taskHref } from "./boardView.js";

const day = (ms) => new Date(ms).toLocaleDateString("en-US", { month: "short", day: "numeric" });

/**
 * @param {import("../domain/task.js").Task[]} tasks  the archived tasks, and the ones that just left (receipts)
 * @param {{
 *   projectId: string,
 *   receipts: Record<string, "restored" | "deleted">,
 *   busy: Record<string, "restoring" | "deleting">,
 *   confirming: string,  the row asking "Delete … forever?", or ""
 *   errors: Record<string, { message: string, code: string, next: string }>,
 * }} ctx
 */
export function toArchivedRows(tasks, { projectId, receipts, busy, confirming, errors, worktrees = {} }) {
  return [...tasks]
    .sort((a, b) => (b.archivedAt || 0) - (a.archivedAt || 0))
    .map((t) => {
      const merge = mergeBadge(t);
      const receipt = receipts[t.id] === "restored" ? `${t.title} restored to Done.` : receipts[t.id] === "deleted" ? `Deleted ${t.title}.` : "";
      return {
        id: t.id,
        title: t.title,
        href: taskHref(projectId, t.id),
        finished: t.statusSince ? `Finished ${day(t.statusSince)}` : "",
        archived: t.archivedAt ? `Archived ${day(t.archivedAt)}` : "",
        hasMerge: merge !== null,
        mergeState: merge?.state ?? "",
        mergeWord: merge?.word ?? "",
        receipt,
        restoredReceipt: receipts[t.id] === "restored",
        busy: Boolean(busy[t.id]),
        busyText: busy[t.id] === "deleting" ? "Deleting…" : busy[t.id] ? "Restoring…" : "",
        noWorktrees: clearableOf(tasks, worktrees, t.id) === 0,
        clearTitle: clearableOf(tasks, worktrees, t.id) === 0 ? "Its worktrees are already cleared" : "Remove this task's worktrees from disk",
        confirming: confirming === t.id,
        confirmTitle: `Delete ${t.title} forever?`,
        error: errors[t.id] ?? null,
      };
    });
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/**
 * How many worktrees are on record under a task: its sessions' and, for a
 * feature, its archived children's. It is what the store knows, so the page
 * paints it without asking git.
 * @param {import("../domain/task.js").Task[]} tasks
 * @param {Record<string, number>} worktrees  worktrees on record, by task
 */
export function clearableOf(tasks, worktrees, id) {
  return (worktrees[id] ?? 0) + tasks.filter((t) => t.parentId === id).reduce((n, t) => n + (worktrees[t.id] ?? 0), 0);
}

/** The ids a clear of one task covers: the task and its archived children. */
export const clearScopeIds = (tasks, id) => [id, ...tasks.filter((t) => t.parentId === id).map((t) => t.id)];

/**
 * The worktrees on record once a clear is done: each task in scope keeps one
 * per session the clear skipped.
 * @param {Record<string, number>} worktrees
 * @param {string[]} ids  the tasks the clear covered
 * @param {{ task_id: string }[]} skipped
 */
export function afterClear(worktrees, ids, skipped) {
  const next = { ...worktrees };
  for (const id of ids) next[id] = skipped.filter((s) => s.task_id === id).length;
  return next;
}

/**
 * The confirm strip of Clear worktrees: what it asks, what it says it will
 * lose, and afterwards what it did and what it skipped.
 * @param {{ label: string, phase: "loading" | "ready" | "clearing" | "done" | "error", preview: { worktrees: number, dirty: number, running: number, shared?: number } | null, result: { cleared: number, skipped: { task_title: string, reason: string, message?: string }[] } | null, error: { message: string, code: string, next: string } | null }} clear
 */
export function toClearStrip(clear) {
  const base = { title: "", lines: [], canConfirm: false, busy: false, closes: false, error: null };
  if (clear.phase === "error") return { ...base, title: `Could not clear worktrees of ${clear.label}`, closes: true, error: clear.error };
  if (clear.phase === "loading") return { ...base, title: `Clear worktrees of ${clear.label}?`, lines: ["Counting worktrees…"], busy: true };
  if (clear.phase === "done") {
    const { cleared, skipped } = clear.result;
    const why = (s) => {
      switch (s.reason) {
        case "running":
          return "still running, so its worktree stays";
        case "shared":
          return "its worktree is shared with a task that is not being cleared, so it stays";
        case "error":
          return `could not tell who is on its worktree (${s.message || "no reason given"}), so it stays`;
        default:
          return `could not be removed: ${s.message ?? "unknown error"}`;
      }
    };
    return { ...base, title: `Cleared ${plural(cleared, "worktree", "worktrees")}`, lines: skipped.map((s) => `${s.task_title}: ${why(s)}.`), closes: true, error: clear.error };
  }
  const { worktrees, dirty, running, shared = 0 } = clear.preview;
  const sharedLine = `${plural(shared, "session shares", "sessions share")} a worktree with a task that is not being cleared; ${shared === 1 ? "its worktree stays" : "their worktrees stay"}.`;
  if (worktrees === 0) {
    const lines = [];
    if (running > 0) lines.push(`${plural(running, "running session keeps", "running sessions keep")} its worktree until it ends.`);
    if (shared > 0) lines.push(sharedLine);
    return { ...base, title: `Nothing to clear in ${clear.label}`, lines: lines.length ? lines : ["Their worktrees are already cleared."], closes: true };
  }
  const lines = [dirty > 0 ? `${plural(dirty, "holds", "hold")} uncommitted changes that will be lost.` : "None hold uncommitted changes."];
  if (running > 0) lines.push(`${plural(running, "running session", "running sessions")} will be skipped.`);
  if (shared > 0) lines.push(sharedLine);
  lines.push("Branches, sessions and transcripts stay. Restoring a task does not bring its worktrees back.");
  return { ...base, title: `Clear ${plural(worktrees, "worktree", "worktrees")} of ${clear.label}?`, lines, canConfirm: clear.phase === "ready", busy: clear.phase === "clearing" };
}

/** The line above the list; "" when nothing is archived (the empty state says it). */
export function archivedSummary(n) {
  if (n === 0) return "";
  return `${n} archived ${n === 1 ? "task" : "tasks"}. Restore puts a task back in Done. Delete removes it and everything it holds.`;
}

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
export function toArchivedRows(tasks, { projectId, receipts, busy, confirming, errors }) {
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
        confirming: confirming === t.id,
        confirmTitle: `Delete ${t.title} forever?`,
        error: errors[t.id] ?? null,
      };
    });
}

/** The line above the list; "" when nothing is archived (the empty state says it). */
export function archivedSummary(n) {
  if (n === 0) return "";
  return `${n} archived ${n === 1 ? "task" : "tasks"}. Restore puts a task back in Done. Delete removes it and everything it holds.`;
}

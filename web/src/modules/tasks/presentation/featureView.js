// The feature section as the markup binds it: a feature task's children as
// rows, each with its page, its status and its documents (as the server
// painted them).

import { STATUSES } from "../domain/task.js";
import { taskHref } from "./boardView.js";

/**
 * @typedef {{ title: string, href: string, updated: string }} DocLink
 */

/** The order the strip's panel lists children in: what waits on the person first. */
const ORDER = ["review", "in_progress", "todo", "backlog", "done"];
const rank = (status) => {
  const i = ORDER.indexOf(status);
  return i < 0 ? ORDER.length : i;
};

const statusLabel = (status) => STATUSES.find((s) => s.value === status)?.label ?? status;

/**
 * @param {import("../domain/task.js").Task[]} children  in list order
 * @param {{ projectId: string, documents: Record<string, DocLink[]> }} ctx  the painted documents by child id
 */
export function toChildRows(children, { projectId, documents }) {
  const ordered = children.map((t, i) => ({ t, i })).sort((a, b) => rank(a.t.status) - rank(b.t.status) || a.i - b.i);
  return ordered.map(({ t }) => {
    const docs = (documents[t.id] ?? []).map((d, i) => ({ ...d, after: i > 0, updatedTitle: `updated ${d.updated}` }));
    return {
      id: t.id,
      title: t.title,
      href: taskHref(projectId, t.id),
      status: t.status,
      statusLabel: statusLabel(t.status),
      done: t.status === "done",
      /** What x-bind sets on the row: present for a done task, absent otherwise. */
      doneAttr: t.status === "done" ? "" : null,
      documents: docs,
      noDocuments: docs.length === 0,
      docCount: docs.length === 0 ? "" : `${docs.length} ${docs.length === 1 ? "doc" : "docs"}`,
    };
  });
}

/**
 * What the strip's Feature button says: how many children, and the phrase
 * for what needs the person or is moving ("3 in review · 2 in progress",
 * "all done", or nothing). name is the button's accessible name: a narrow
 * strip drops the phrase from sight, not from the name.
 * @param {{ status: string }[]} rows
 */
export function featureSummary(rows) {
  const count = (status) => rows.filter((r) => r.status === status).length;
  const parts = [];
  if (count("review") > 0) parts.push(`${count("review")} in review`);
  if (count("in_progress") > 0) parts.push(`${count("in_progress")} in progress`);
  if (parts.length === 0 && rows.length > 0 && count("done") === rows.length) parts.push("all done");
  const phrase = parts.join(" · ");
  const children = `${rows.length} ${rows.length === 1 ? "child" : "children"}`;
  return { has: rows.length > 0, count: String(rows.length), phrase, name: ["Feature", children, ...parts].join(", ") };
}

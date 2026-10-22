// The feature section as the markup binds it: a feature task's children as
// rows, each with its page, its status and its documents (as the server
// painted them).

import { STATUSES } from "../domain/task.js";
import { taskHref } from "./boardView.js";

/**
 * @typedef {{ title: string, href: string, updated: string }} DocLink
 */

const statusLabel = (status) => STATUSES.find((s) => s.value === status)?.label ?? status;

/**
 * @param {import("../domain/task.js").Task[]} children  in list order
 * @param {{ projectId: string, documents: Record<string, DocLink[]> }} ctx  the painted documents by child id
 */
export function toChildRows(children, { projectId, documents }) {
  return children.map((t) => {
    const docs = (documents[t.id] ?? []).map((d, i) => ({ ...d, after: i > 0, updatedTitle: `updated ${d.updated}` }));
    return {
      id: t.id,
      title: t.title,
      href: taskHref(projectId, t.id),
      status: t.status,
      statusLabel: statusLabel(t.status),
      documents: docs,
      noDocuments: docs.length === 0,
    };
  });
}

// Review requests as the markup binds them: a card per request, in the task
// page's review band and in the project's inbox.

import { count, relativeTime } from "../../../shared/presentation/format.js";

/**
 * Names the page knows when it loads (the reviews seed): sessions by id as
 * "go-developer · Server: architect channel", tasks by id with their page,
 * documents by id.
 * @typedef {{
 *   sessions: Record<string, string>,
 *   tasks: Record<string, { title: string, href: string }>,
 *   documents: Record<string, string>,
 * }} Names
 */

const STATE_WORD = { pending: "waiting on you", approved: "approved", changes_requested: "changes requested", withdrawn: "withdrawn" };

/** A body this long is folded until shown in full. */
const LONG_LINES = 8;
const LONG_CHARS = 800;

/**
 * @param {import("../domain/review.js").ReviewRequest} r
 * @param {{ names: Names, projectId: string, now: Date }} ctx
 */
export function toCard(r, { names, projectId, now }) {
  const p = encodeURIComponent(projectId);
  const t = encodeURIComponent(r.taskId);
  return {
    id: r.id,
    taskId: r.taskId,
    subject: r.subject || "Review request",
    body: r.body,
    long: r.body.split("\n").length > LONG_LINES || r.body.length > LONG_CHARS,
    byline: r.aboutSessionId ? `from the architect · about ${names.sessions[r.aboutSessionId] ?? "a session"}` : "from the architect",
    age: relativeTime(r.createdAt, now),
    state: r.state,
    stateWord: STATE_WORD[r.state],
    note: r.responseNote,
    documents: r.documentIds.map((id) => ({ id, title: names.documents[id] || "Document", href: `/projects/${p}/tasks/${t}/documents/${encodeURIComponent(id)}` })),
    artifacts: r.artifacts.map((a) => ({ id: a.id, title: `Artifact ${a.id.slice(0, 8)}`, href: a.href })),
    get hasLinks() {
      return this.documents.length + this.artifacts.length > 0;
    },
  };
}

/** "1 review waits on you", "3 reviews wait on you". */
export const waitingLine = (n) => `${count(n, "review")} ${n === 1 ? "waits" : "wait"} on you`;

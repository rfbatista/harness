// TaskGateway in memory, for tests and web/dev pages; it obeys the same
// contract as the real gateway (../testing/gateway-contract.js).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { isStatus } from "../domain/task.js";
import { toTask } from "./dto.js";

/**
 * @param {{
 *   projects?: string[],
 *   tasks?: import("../domain/task.js").Task[],
 *   sessions?: { ticketId: string, status: string }[],
 *   documents?: { id: string, ticketId: string, updatedAt: string, scope?: "task" | "project" }[],
 * }} [world]
 */
export function memoryTasks({ projects = [], tasks = [], sessions = [], documents = [] } = {}) {
  const known = new Set([...projects, ...tasks.map((t) => t.projectId)]);
  const store = new Map(tasks.map((t) => [t.id, t]));
  let next = 1;
  /** Every updateTask input, as given (only the keys present): what a component sent. */
  const calls = [];
  /** A test hook: when set, updateTask throws what it returns. */
  let refuse = null;
  /** Each archive is a moment after the last, so archived tasks order newest first. */
  let clock = Date.parse("2026-10-19T12:00:00Z");
  const now = () => (clock += 1000);

  const check = (title, status) => {
    if (!title?.trim()) throw new StructuredError(Codes.INVALID_INPUT, "title is required", 400);
    if (!isStatus(status)) throw new StructuredError(Codes.INVALID_STATUS, "invalid ticket status", 400);
  };

  /** The server's parent invariant: same project, one level, never itself (TICKET_PARENT_INVALID). */
  const checkParent = (projectId, parentId, selfId = "") => {
    if (!parentId) return;
    const parent = store.get(parentId);
    const invalid = !parent || parent.projectId !== projectId || parentId === selfId || parent.parentId;
    if (invalid) throw new StructuredError(Codes.TICKET_PARENT_INVALID, "parent must be a top-level task of the same project", 400);
  };

  /** @type {import("../domain/ports.js").TaskGateway} */
  const gateway = {
    decodeTask: toTask,

    async createTask({ projectId, title, description, status, parentId }) {
      check(title, status);
      if (!known.has(projectId)) throw new StructuredError(Codes.PROJECT_NOT_FOUND, "project not found", 404);
      checkParent(projectId, parentId);
      const task = Object.freeze({ id: `task-${next++}`, projectId, title: title.trim(), description: description ?? "", status, parentId: parentId ?? "" });
      store.set(task.id, task);
      return task;
    },

    async updateTask({ id, title, description, status, parentId }) {
      calls.push({ updateTask: { id, ...(title !== undefined && { title }), ...(description !== undefined && { description }), ...(status !== undefined && { status }), ...(parentId !== undefined && { parentId }) } });
      if (refuse) throw refuse();
      const task = store.get(id);
      if (!task) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      if (title !== undefined && !title.trim()) throw new StructuredError(Codes.INVALID_INPUT, "title is required", 400);
      if (status !== undefined && task.archivedAt) throw new StructuredError(Codes.TASK_ARCHIVED, "the task is archived; restore it before changing its status", 409);
      if (status !== undefined && !isStatus(status)) throw new StructuredError(Codes.INVALID_INPUT, "status must be one of backlog, todo, in_progress, review, done", 400);
      if (parentId !== undefined) checkParent(task.projectId, parentId, id);
      const updated = Object.freeze({
        ...task,
        ...(title !== undefined && { title: title.trim() }),
        ...(description !== undefined && { description }),
        ...(status !== undefined && { status }),
        ...(parentId !== undefined && { parentId }),
      });
      store.set(id, updated);
      return updated;
    },

    moveTask(id, status) {
      return gateway.updateTask({ id, status });
    },

    async archiveTask(id) {
      const task = store.get(id);
      if (!task) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      if (task.archivedAt) return task;
      if (task.status !== "done") throw new StructuredError(Codes.TASK_NOT_DONE, "only a done task can be archived", 409);
      // A feature task is archived with its children (ADR 0059), and only when all of them are done.
      const kids = [...store.values()].filter((t) => t.parentId === id && !t.archivedAt);
      const open = kids.filter((t) => t.status !== "done");
      if (open.length > 0) {
        const message = `${open.length} ${open.length === 1 ? "task is" : "tasks are"} not done: ${open.map((t) => t.title).join(", ")}`;
        throw new StructuredError(Codes.FEATURE_HAS_OPEN_CHILDREN, message, 409, { open_children: open.map((t) => ({ id: t.id, title: t.title, status: t.status })) });
      }
      const at = now();
      for (const kid of kids) store.set(kid.id, Object.freeze({ ...kid, archivedAt: at }));
      const archived = Object.freeze({ ...task, archivedAt: at });
      store.set(id, archived);
      return archived;
    },

    async archiveDoneTasks(projectId) {
      // What the Done column shows: done plain tasks, done orphan children and done features with every child done.
      const all = [...store.values()].filter((t) => t.projectId === projectId);
      const onBoard = new Set(all.filter((t) => !t.archivedAt).map((t) => t.id));
      const cards = all.filter((t) => t.status === "done" && !t.archivedAt && !onBoard.has(t.parentId));
      const ids = [];
      let skipped = 0;
      for (const card of cards) {
        const before = store.get(card.id);
        try {
          await gateway.archiveTask(card.id);
        } catch (err) {
          if (err.code !== Codes.FEATURE_HAS_OPEN_CHILDREN) throw err;
          skipped++;
          continue;
        }
        ids.push(card.id, ...[...store.values()].filter((t) => t.parentId === before.id && t.archivedAt === store.get(before.id).archivedAt).map((t) => t.id));
      }
      return { archived: ids.length, ids, skipped };
    },

    async restoreTask(id) {
      const task = store.get(id);
      if (!task) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      if (!task.archivedAt) return task;
      const feature = store.get(task.parentId);
      if (feature?.archivedAt) {
        throw new StructuredError(Codes.FEATURE_ARCHIVED, `Restore the feature \u201c${feature.title}\u201d first.`, 409, { feature: { id: feature.id, title: feature.title } });
      }
      // A feature comes back with the children archived with it.
      for (const kid of store.values()) if (kid.parentId === id && kid.archivedAt === task.archivedAt) store.set(kid.id, Object.freeze({ ...kid, archivedAt: 0 }));
      const restored = Object.freeze({ ...task, archivedAt: 0 });
      store.set(id, restored);
      return restored;
    },

    async deleteTask(id) {
      if (!store.delete(id)) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
    },

    async listDocumentVersions(taskId) {
      return documents.filter((d) => d.ticketId === taskId).map((d) => ({ id: d.id, version: d.updatedAt }));
    },

    async setDocumentScope(documentId, scope) {
      if (scope !== "task" && scope !== "project") throw new StructuredError(Codes.INVALID_INPUT, "scope must be task or project", 400);
      const doc = documents.find((d) => d.id === documentId);
      if (!doc) throw new StructuredError(Codes.DOCUMENT_NOT_FOUND, "document not found", 404);
      if ((doc.scope ?? "task") !== scope) {
        doc.scope = scope;
        doc.updatedAt = new Date(Date.parse(doc.updatedAt) + 1000).toISOString(); // a move is a new version
      }
      return { id: doc.id, version: doc.updatedAt, scope: doc.scope ?? "task" };
    },

    async countSessions(_projectId, taskId) {
      const mine = sessions.filter((s) => s.ticketId === taskId);
      return { total: mine.length, live: mine.filter((s) => !["done", "failed", "stopped"].includes(s.status)).length };
    },
  };

  return {
    gateway,
    calls,
    get refuse() {
      return refuse;
    },
    set refuse(fn) {
      refuse = fn;
    },
    tasks: () => [...store.values()],
    sessions,
    documents,
    /** An agent wrote or rewrote a document on a task. */
    writeDocument(doc) {
      const at = documents.findIndex((d) => d.id === doc.id);
      if (at >= 0) documents[at] = doc;
      else documents.push(doc);
    },
  };
}

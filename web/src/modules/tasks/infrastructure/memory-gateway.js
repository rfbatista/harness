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

  const check = (title, status) => {
    if (!title?.trim()) throw new StructuredError(Codes.INVALID_INPUT, "title is required", 400);
    if (!isStatus(status)) throw new StructuredError(Codes.INVALID_STATUS, "invalid ticket status", 400);
  };

  /** @type {import("../domain/ports.js").TaskGateway} */
  const gateway = {
    decodeTask: toTask,

    async createTask({ projectId, title, description, status }) {
      check(title, status);
      if (!known.has(projectId)) throw new StructuredError(Codes.PROJECT_NOT_FOUND, "project not found", 404);
      const task = Object.freeze({ id: `task-${next++}`, projectId, title: title.trim(), description: description ?? "", status });
      store.set(task.id, task);
      return task;
    },

    async updateTask({ id, title, description, status }) {
      calls.push({ updateTask: { id, ...(title !== undefined && { title }), ...(description !== undefined && { description }), ...(status !== undefined && { status }) } });
      if (refuse) throw refuse();
      const task = store.get(id);
      if (!task) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      if (title !== undefined && !title.trim()) throw new StructuredError(Codes.INVALID_INPUT, "title is required", 400);
      if (status !== undefined && !isStatus(status)) throw new StructuredError(Codes.INVALID_INPUT, "status must be one of backlog, todo, in_progress, review, done", 400);
      const updated = Object.freeze({
        ...task,
        ...(title !== undefined && { title: title.trim() }),
        ...(description !== undefined && { description }),
        ...(status !== undefined && { status }),
      });
      store.set(id, updated);
      return updated;
    },

    moveTask(id, status) {
      return gateway.updateTask({ id, status });
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

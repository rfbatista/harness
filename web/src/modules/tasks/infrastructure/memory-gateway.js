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
 * }} [world]
 */
export function memoryTasks({ projects = [], tasks = [], sessions = [] } = {}) {
  const known = new Set([...projects, ...tasks.map((t) => t.projectId)]);
  const store = new Map(tasks.map((t) => [t.id, t]));
  let next = 1;

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
      const task = store.get(id);
      if (!task) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
      check(title, status);
      const updated = Object.freeze({ ...task, title: title.trim(), description: description ?? "", status });
      store.set(id, updated);
      return updated;
    },

    async deleteTask(id) {
      if (!store.delete(id)) throw new StructuredError(Codes.TICKET_NOT_FOUND, "ticket not found", 404);
    },

    async countSessions(_projectId, taskId) {
      const mine = sessions.filter((s) => s.ticketId === taskId);
      return { total: mine.length, live: mine.filter((s) => !["done", "failed", "stopped"].includes(s.status)).length };
    },
  };

  return { gateway, tasks: () => [...store.values()], sessions };
}

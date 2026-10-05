// TaskGateway over the harness HTTP API (RPC-style ticket routes).

import { toTask } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").TaskGateway}
 */
export function tasksGateway(api) {
  return {
    decodeTask: toTask,

    async createTask({ projectId, title, description, status }) {
      const body = await api.post("/create_ticket", { project_id: projectId, title, description, status });
      return toTask(body?.ticket);
    },

    async updateTask({ id, title, description, status }) {
      const body = await api.post("/update_ticket", { ticket_id: id, title, description, status });
      return toTask(body?.ticket);
    },

    async deleteTask(id) {
      await api.post("/delete_ticket", { ticket_id: id });
    },

    async countSessions(projectId, taskId) {
      const body = await api.get("/sessions", { project_id: projectId, ticket_id: taskId });
      const sessions = Array.isArray(body?.sessions) ? body.sessions : [];
      const ended = new Set(["done", "failed", "stopped"]);
      return { total: sessions.length, live: sessions.filter((s) => !ended.has(s.status)).length };
    },
  };
}

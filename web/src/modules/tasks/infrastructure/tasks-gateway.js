// TaskGateway over the harness HTTP API (RPC-style ticket routes).

import { Codes, StructuredError } from "../../../shared/domain/errors.js";
import { toTask } from "./dto.js";

function bad(detail) {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected document data: ${detail}.`);
}

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").TaskGateway}
 */
export function tasksGateway(api) {
  /** @type {import("../domain/ports.js").TaskGateway} */
  const gateway = {
    decodeTask: toTask,

    async createTask({ projectId, title, description, status }) {
      const body = await api.post("/create_ticket", { project_id: projectId, title, description, status });
      return toTask(body?.ticket);
    },

    async updateTask({ id, title, description, status }) {
      const body = { ticket_id: id };
      if (title !== undefined) body.title = title;
      if (description !== undefined) body.description = description;
      if (status !== undefined) body.status = status;
      const res = await api.post("/update_ticket", body);
      return toTask(res?.ticket);
    },

    moveTask(id, status) {
      return gateway.updateTask({ id, status });
    },

    async deleteTask(id) {
      await api.post("/delete_ticket", { ticket_id: id });
    },

    async listDocumentVersions(taskId) {
      const body = await api.get("/list_ticket_documents", { ticket_id: taskId });
      if (!Array.isArray(body?.documents)) bad("expected {documents: [...]}");
      return body.documents.map((d) => {
        if (typeof d?.id !== "string" || typeof d.updated_at !== "string") bad("document without id or updated_at");
        return { id: d.id, version: d.updated_at };
      });
    },

    async countSessions(projectId, taskId) {
      const body = await api.get("/sessions", { project_id: projectId, ticket_id: taskId });
      const sessions = Array.isArray(body?.sessions) ? body.sessions : [];
      const ended = new Set(["done", "failed", "stopped"]);
      return { total: sessions.length, live: sessions.filter((s) => !ended.has(s.status)).length };
    },
  };
  return gateway;
}

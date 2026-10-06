// A stand-in for the server's ticket routes and GET /api/sessions, backed by
// the memory gateway and speaking the real wire format.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryTasks } from "../infrastructure/memory-gateway.js";
import { taskDTO } from "./fixtures.js";

const STATUS = { PROJECT_NOT_FOUND: 404, TICKET_NOT_FOUND: 404, INVALID_INPUT: 400, INVALID_STATUS: 400 };

export function stubTasksApi(world) {
  const memory = memoryTasks(world);
  const { gateway } = memory;

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "POST /api/create_ticket": {
          const t = await gateway.createTask({ projectId: body.project_id, title: body.title, description: body.description, status: body.status });
          return jsonResponse(200, { ticket: taskDTO(t) });
        }
        case "POST /api/update_ticket": {
          const t = await gateway.updateTask({ id: body.ticket_id, title: body.title, description: body.description, status: body.status });
          return jsonResponse(200, { ticket: taskDTO(t) });
        }
        case "POST /api/delete_ticket":
          await gateway.deleteTask(body.ticket_id);
          return new Response(null, { status: 204 });
        case "GET /api/sessions": {
          const ticket = url.searchParams.get("ticket_id");
          const sessions = memory.sessions.filter((s) => s.ticketId === ticket).map((s, i) => ({ id: `s${i}`, ticket_id: s.ticketId, status: s.status }));
          return jsonResponse(200, { sessions });
        }
        case "GET /api/list_ticket_documents": {
          const ticket = url.searchParams.get("ticket_id");
          const documents = memory.documents
            .filter((d) => d.ticketId === ticket)
            .map((d) => ({ id: d.id, project_id: "p1", title: d.id, format: d.format ?? "markdown", content: "…", updated_at: d.updatedAt }));
          return jsonResponse(200, { documents });
        }
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  return { fetch, memory };
}

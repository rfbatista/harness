// A stand-in for the harness server's session routes, backed by the memory
// gateway: a fetch and an EventSource that speak the real wire format
// (httpapi/sessions.go, httpapi/events.go). The real sessionsGateway runs the
// contract suite against it, which proves its HTTP mapping and error handling.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryGateway } from "../infrastructure/memory-gateway.js";
import { toDTO } from "./fixtures.js";

const STATUS = {
  PROJECT_NOT_FOUND: 404,
  SESSION_NOT_FOUND: 404,
  SESSION_NOT_RUNNING: 409,
  INVALID_INPUT: 400,
  REPOSITORY_NOT_FOUND: 404,
  CROSS_PROJECT_ACCESS: 400,
};

export function stubApi(world) {
  const memory = memoryGateway(world);
  const { gateway } = memory;

  const fail = (err) => jsonResponse(STATUS[codeOf(err)] ?? 500, { error: err.message, code: codeOf(err) });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/sessions") {
        const list = await gateway.list({
          projectId: url.searchParams.get("project_id") ?? "",
          ticketId: url.searchParams.get("ticket_id") ?? undefined,
        });
        return jsonResponse(200, { sessions: list.map(toDTO) });
      }
      if (method === "GET" && url.pathname === "/api/list_branches") {
        const list = await gateway.listBranches(url.searchParams.get("repository_id") ?? "");
        return jsonResponse(200, { branches: list.map((b) => ({ name: b.name, remote: b.remote, is_head: b.isHead })) });
      }
      if (method === "POST" && url.pathname === "/api/start_interactive_session") {
        if (body.runs_on !== "server") return jsonResponse(400, { error: "the web client runs sessions on the server", code: "INVALID_INPUT" });
        const session = await gateway.start({
          projectId: body.project_id,
          ticketId: body.ticket_id,
          repositoryId: body.repository_id,
          agentId: body.agent_id,
          prompt: body.prompt,
          autoAccept: body.auto_accept,
          size: body.size,
        });
        return jsonResponse(201, { session: toDTO(session), agent: {} });
      }
      let m = url.pathname.match(/^\/api\/sessions\/([^/]+)$/);
      if (method === "DELETE" && m) {
        await gateway.remove(decodeURIComponent(m[1]));
        return new Response(null, { status: 204 });
      }
      m = url.pathname.match(/^\/api\/sessions\/([^/]+)\/stop$/);
      if (method === "POST" && m) {
        await gateway.stop(decodeURIComponent(m[1]));
        return new Response(null, { status: 202 });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.unfollow = gateway.follow(
        url.searchParams.get("project_id") ?? "",
        (change) => {
          const data =
            change.kind === "deleted"
              ? { session: { id: change.id }, deleted: true }
              : { session: toDTO(change.session) };
          this.onmessage?.({ data: JSON.stringify(data) });
        },
        () => {},
      );
      queueMicrotask(() => this.onopen?.({}));
    }
    close() {
      this.unfollow();
    }
  }

  return { fetch, EventSource: StubEventSource, memory };
}

// A stand-in for the harness server's session routes, backed by the memory
// gateway: a fetch and an EventSource that speak the real wire format
// (httpapi/sessions.go, httpapi/events.go). The real sessionsGateway runs the
// contract suite against it, which proves its HTTP mapping and error handling.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryGateway } from "../infrastructure/memory-gateway.js";
import { toDTO } from "./fixtures.js";

const STATUS = { PROJECT_NOT_FOUND: 404, SESSION_NOT_FOUND: 404, SESSION_NOT_RUNNING: 409 };

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
        const list = await gateway.list(url.searchParams.get("project_id") ?? "");
        return jsonResponse(200, { sessions: list.map(toDTO) });
      }
      let m = url.pathname.match(/^\/api\/sessions\/([^/]+)\/messages$/);
      if (method === "POST" && m) {
        await gateway.send(decodeURIComponent(m[1]), body.text);
        return new Response(null, { status: 202 });
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

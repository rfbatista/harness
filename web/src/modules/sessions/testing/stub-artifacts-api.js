// A stand-in for the server's artifact routes, backed by the memory gateway
// and speaking the real wire format: GET /api/artifacts?session_id= and the
// `artifact` / `done` events on GET /api/sessions/:id/events. The real
// artifactsGateway runs the contract suite against it.

import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryArtifacts } from "../infrastructure/memory-artifacts.js";
import { toArtifactDTO } from "./artifact-fixtures.js";

export function stubArtifactsApi(world) {
  const memory = memoryArtifacts(world);
  const { gateway } = memory;
  const sources = new Set();
  let seq = 0;

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    if (method === "GET" && url.pathname === "/api/artifacts") {
      const sessionId = url.searchParams.get("session_id");
      const ticketId = url.searchParams.get("ticket_id");
      if (!sessionId && !ticketId) return jsonResponse(400, { error: "session_id or ticket_id is required", code: "INVALID_INPUT" });
      const list = await gateway.list(sessionId ?? "");
      return jsonResponse(200, { artifacts: list.map(toArtifactDTO) });
    }
    return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      const m = url.pathname.match(/^\/api\/sessions\/([^/]+)\/events$/);
      this.sessionId = m ? decodeURIComponent(m[1]) : "";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(
        this.sessionId,
        (event) => {
          if (event.kind === "published") {
            const a = event.artifact;
            this.send({ seq: ++seq, session_id: a.sessionId, type: "artifact", text: a.note, artifact: toArtifactDTO(a), at: a.updatedAt.toISOString() });
          } else {
            this.send({ seq: ++seq, session_id: this.sessionId, type: "done", at: new Date().toISOString() });
          }
        },
        () => {},
      );
      queueMicrotask(() => {
        this.onopen?.({});
        // Interactive sessions put lifecycle events on the same stream; the gateway must ignore them.
        this.send({ seq: ++seq, session_id: this.sessionId, type: "status", status: "running", at: new Date().toISOString() });
      });
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      this.unfollow();
      sources.delete(this);
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    /** Pushes any line (an object, or a raw string) to every stream open on the session, as a buggy server might. */
    pushRaw(sessionId, dto) {
      for (const s of sources) if (s.sessionId === sessionId) s.send(dto);
    },
  };
}

// A stand-in for the server's artifact routes, backed by the memory gateway
// and speaking the real wire format: GET /api/artifacts (by session_id,
// ticket_id or project_id, narrowed by scope), POST /api/set_artifact_scope,
// POST /api/attach_artifact_to_ticket and /api/detach_artifact_from_ticket,
// DELETE /api/artifacts/:id, the `artifact` / `done` events on
// GET /api/sessions/:id/events, and the `artifact` changes on the project
// feed (GET /api/events?project_id=). The real artifactsGateway runs the
// contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryArtifacts } from "../infrastructure/memory-artifacts.js";
import { toArtifactDTO } from "./artifact-fixtures.js";

export function stubArtifactsApi(world) {
  const memory = memoryArtifacts(world);
  const { gateway } = memory;
  const sources = new Set();
  let seq = 0;

  async function fetch(input, init = {}) {
    try {
      return await route(new URL(input, "http://harness.test"), init.method ?? "GET", init.body ? JSON.parse(init.body) : {});
    } catch (err) {
      return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  async function route(url, method, body) {
    if (method === "GET" && url.pathname === "/api/artifacts") {
      const sessionId = url.searchParams.get("session_id");
      const ticketId = url.searchParams.get("ticket_id");
      const projectId = url.searchParams.get("project_id");
      const scope = url.searchParams.get("scope");
      if (!sessionId && !ticketId && !projectId) return jsonResponse(400, { error: "session_id, ticket_id or project_id is required", code: "INVALID_INPUT" });
      if (scope && scope !== "task" && scope !== "project") return jsonResponse(400, { error: "scope must be task or project", code: "INVALID_INPUT" });
      let list;
      if (sessionId) list = await gateway.list(sessionId);
      else if (ticketId) list = await gateway.listTask(ticketId);
      else if (projectId && scope === "project") list = await gateway.listProject(projectId);
      else return jsonResponse(400, { error: "the stub serves session_id, ticket_id, or project_id with scope=project", code: "INVALID_INPUT" });
      if (scope) list = list.filter((a) => a.scope === scope);
      return jsonResponse(200, { artifacts: list.map(toArtifactDTO) });
    }
    if (method === "POST" && url.pathname === "/api/set_artifact_scope") {
      return jsonResponse(200, { artifact: toArtifactDTO(await gateway.setScope(body.artifact_id, body.scope)) });
    }
    if (method === "POST" && url.pathname === "/api/attach_artifact_to_ticket") {
      return jsonResponse(200, { artifact: toArtifactDTO(await gateway.attach(body.artifact_id, body.ticket_id)) });
    }
    if (method === "POST" && url.pathname === "/api/detach_artifact_from_ticket") {
      return jsonResponse(200, { artifact: toArtifactDTO(await gateway.detach(body.artifact_id, body.ticket_id)) });
    }
    const m = url.pathname.match(/^\/api\/artifacts\/([^/]+)$/);
    if (method === "DELETE" && m) {
      await gateway.remove(decodeURIComponent(m[1]));
      return new Response(null, { status: 204 });
    }
    return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
  }

  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      const m = url.pathname.match(/^\/api\/sessions\/([^/]+)\/events$/);
      this.sessionId = m ? decodeURIComponent(m[1]) : "";
      this.projectId = url.pathname === "/api/events" ? (url.searchParams.get("project_id") ?? "") : "";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      if (this.projectId) {
        this.followProject();
        return;
      }
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
    /** The project feed: artifact changes, among the session and ticket changes the gateway must ignore. */
    followProject() {
      this.unfollow = gateway.followProject(
        this.projectId,
        (change) => {
          if (change.kind === "deleted") {
            this.send({ artifact: { id: change.id, project_id: change.projectId, ticket_id: change.ticketId, attached_ticket_ids: change.attachedTicketIds }, deleted: true });
          } else {
            this.send({ artifact: toArtifactDTO(change.artifact) });
          }
        },
        () => {},
      );
      queueMicrotask(() => {
        this.onopen?.({});
        this.send({ session: { id: "s-other", ticket_id: "t1", status: "running" } });
        this.send({ ticket: { id: "t1" }, deleted: true });
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
    /** Pushes any line to every project feed open on the project. */
    pushProject(projectId, dto) {
      for (const s of sources) if (s.projectId === projectId) s.send(dto);
    },
  };
}

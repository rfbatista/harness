// A stand-in for the server's artifact routes, backed by the memory gateway
// and speaking the real wire format: GET /api/artifacts (by session_id,
// ticket_id or project_id, narrowed by scope), POST /api/set_artifact_scope,
// POST /api/attach_artifact_to_ticket and /api/detach_artifact_from_ticket,
// DELETE /api/artifacts/:id, and the page event stream
// (GET /api/page_events?project_id=): the `artifact` changes of its project
// kind, and the `artifact` / `done` events of the session it follows
// (POST /api/follow_session), replayed from the start or after after_seq as
// the server's hub does. The real artifactsGateway runs the contract suite
// against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { helloEnvelope, pageEventsProject, projectEnvelope, sessionEnvelope } from "../../../shared/testing/page-events.js";
import { memoryArtifacts } from "../infrastructure/memory-artifacts.js";
import { toArtifactDTO } from "./artifact-fixtures.js";

export function stubArtifactsApi(world) {
  const memory = memoryArtifacts(world);
  const { gateway } = memory;
  const sources = new Set();
  let seq = 0;
  let streams = 0;
  /** Session id → every event it put on its stream, as the hub keeps them for replay. */
  const logs = new Map();

  /** A session's event: logged, and sent to every stream following the session. */
  function emit(sessionId, event) {
    const dto = { seq: ++seq, session_id: sessionId, ...event, at: event.at ?? new Date().toISOString() };
    if (!logs.has(sessionId)) logs.set(sessionId, []);
    logs.get(sessionId).push(dto);
    for (const s of sources) if (s.sessionId === sessionId) s.send(sessionEnvelope(sessionId, dto));
  }
  /** Artifact id → its scope, to tell a move from the same scope again. */
  const scopes = new Map((world.artifacts ?? []).map((a) => [a.id, a.scope]));
  function published(a) {
    scopes.set(a.id, a.scope);
    emit(a.sessionId, { type: "artifact", text: a.note, artifact: toArtifactDTO(a), at: a.updatedAt.toISOString() });
  }

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
    if (method === "POST" && url.pathname === "/api/follow_session") {
      const source = [...sources].find((s) => s.streamId === body.stream_id);
      if (!source) return jsonResponse(404, { error: "page event stream not found", code: "STREAM_NOT_FOUND" });
      source.follow(body.session_id ?? "", body.after_seq ?? 0);
      return new Response(null, { status: 204 });
    }
    if (method === "POST" && url.pathname === "/api/set_artifact_scope") {
      const moved = await gateway.setScope(body.artifact_id, body.scope);
      // A move is on the producing session's stream; the same scope again is no move.
      if (scopes.get(moved.id) !== moved.scope) published(moved);
      return jsonResponse(200, { artifact: toArtifactDTO(moved) });
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
      this.projectId = pageEventsProject(input) ?? "";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.streamId = `stream-${++streams}`;
      this.sessionId = "";
      sources.add(this);
      this.unfollowProject = () => {};
      if (this.projectId) this.followProject();
      queueMicrotask(() => {
        if (!sources.has(this)) return;
        this.onopen?.({});
        this.send(helloEnvelope(this.streamId));
        if (!this.projectId) return;
        this.send(projectEnvelope({ session: { id: "s-other", ticket_id: "t1", status: "running" } }));
        this.send(projectEnvelope({ ticket: { id: "t1" }, deleted: true }));
      });
    }
    /** Follows a session ("" for none): its events after afterSeq, then live ones. */
    follow(sessionId, afterSeq) {
      this.sessionId = "";
      if (!sessionId) return;
      // Interactive sessions put lifecycle events on the same stream; the gateway must ignore them.
      if (!logs.has(sessionId)) emit(sessionId, { type: "status", status: "running" });
      this.sessionId = sessionId;
      for (const dto of logs.get(sessionId)) if (dto.seq > afterSeq) this.send(sessionEnvelope(sessionId, dto));
    }
    /** The page event stream's project changes: artifact changes, among the session and ticket changes the gateway must ignore. */
    followProject() {
      this.unfollowProject = gateway.followProject(
        this.projectId,
        (change) => {
          if (change.kind === "deleted") {
            this.send(projectEnvelope({ artifact: { id: change.id, project_id: change.projectId, ticket_id: change.ticketId, attached_ticket_ids: change.attachedTicketIds }, deleted: true }));
          } else {
            this.send(projectEnvelope({ artifact: toArtifactDTO(change.artifact) }));
          }
        },
        () => {},
      );
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      this.unfollowProject();
      sources.delete(this);
    }
    /** The server ends the stream, as when a part of it falls behind. */
    drop() {
      this.close();
      this.onerror?.({});
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory: {
      ...memory,
      publish(input) {
        const a = memory.publish(input);
        published(a);
        return a;
      },
      end(sessionId) {
        memory.end(sessionId);
        emit(sessionId, { type: "done" });
      },
    },
    /** Ends every open page event stream, as the server does when a part of it falls behind. */
    dropStreams() {
      for (const s of [...sources]) s.drop();
    },
    /** Pushes any event (an object, or as a string any line) to every stream following the session, as a buggy server might. */
    pushRaw(sessionId, dto) {
      for (const s of sources) if (s.sessionId === sessionId) s.send(typeof dto === "string" ? dto : sessionEnvelope(sessionId, dto));
    },
    /** Pushes any project change (or, as a string, any line) to every stream open on the project. */
    pushProject(projectId, dto) {
      for (const s of sources) if (s.projectId === projectId) s.send(typeof dto === "string" ? dto : projectEnvelope(dto));
    },
  };
}

// A stand-in for the harness server's /api/code/* routes and the page event
// stream's project kind, backed by the memory gateway's wire answers: a fetch and an
// EventSource that speak the contract's format. The real codeGateway runs
// the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { helloEnvelope, projectEnvelope } from "../../../shared/testing/page-events.js";
import { memoryCode } from "../infrastructure/memory-gateway.js";

export function stubCodeApi(world) {
  const memory = memoryCode(world);
  const { wire } = memory;
  const sources = new Set();
  const requested = [];

  const src = (q) => ({ repositoryId: q.get("repository_id") ?? "", ref: q.get("ref") ?? "", workspaceId: q.get("workspace_id") ?? "" });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const q = url.searchParams;
    requested.push(url.pathname + url.search);
    try {
      if ((init.method ?? "GET") !== "GET") return jsonResponse(405, { error: "read only", code: "INVALID_INPUT" });
      switch (url.pathname) {
        case "/api/code/tree":
          return jsonResponse(200, wire.tree(src(q), q.get("path") ?? ""));
        case "/api/code/paths":
          return jsonResponse(200, wire.paths(src(q)));
        case "/api/code/file":
          return jsonResponse(200, wire.file(src(q), q.get("path") ?? ""));
        case "/api/code/map":
          return jsonResponse(200, wire.map(src(q)));
        case "/api/code/diff":
          return jsonResponse(
            200,
            wire.diff({
              workspaceId: q.get("workspace_id") ?? "",
              scope: q.get("scope") ?? "",
              repositoryId: q.get("repository_id") ?? "",
              commit: q.get("commit") ?? "",
              base: q.get("base") ?? "",
              head: q.get("head") ?? "",
              path: q.get("path") ?? "",
            }),
          );
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err) });
    }
  }

  // The project's changes carry every kind of change; the gateway must pick out code_changed.
  class EventSource {
    constructor(url) {
      this.url = url;
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      queueMicrotask(() => {
        this.onopen?.({});
        this.onmessage?.({ data: JSON.stringify(helloEnvelope()) });
      });
    }
    close() {
      sources.delete(this);
    }
  }

  const send = (data) => {
    for (const s of sources) s.onmessage?.({ data: typeof data === "string" ? data : JSON.stringify(projectEnvelope(data)) });
  };

  return {
    fetch,
    EventSource,
    memory,
    requested,
    /** Sends a project change (or, as a string, any line) as the server would. */
    send,
    /** Edits a worktree and sends its code_changed on the stream. */
    change(workspaceId, files, opts) {
      memory.change(workspaceId, files, opts);
      send({ code_changed: { workspace_id: workspaceId, head: world.workspaces[workspaceId].head } });
    },
  };
}

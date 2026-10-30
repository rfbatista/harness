// A stand-in for the harness server's git state routes, backed by the memory
// gateway: a fetch and an EventSource that speak the contract's wire format.
// The real syncGateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { toGitStateDTO } from "../infrastructure/dto.js";
import { memorySync } from "../infrastructure/memory-gateway.js";

export function stubSyncApi(world) {
  const memory = memorySync(world);
  const { gateway } = memory;
  const sources = new Set();
  /** The bodies the gateway posted, oldest first, with their route. */
  const posted = [];

  function fail(err) {
    const details = err.details?.state ? { ...err.details, state: toGitStateDTO(err.details.state) } : err.details;
    return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err), details });
  }

  const missing = (field) => jsonResponse(400, { error: `INVALID_INPUT: ${field} is required`, code: "INVALID_INPUT" });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/worktree_git_state") {
        const id = url.searchParams.get("workspace_id");
        if (!id) return missing("workspace_id");
        return jsonResponse(200, { state: toGitStateDTO(await gateway.get(id)) });
      }
      if (method === "GET" && url.pathname === "/api/list_worktree_git_states") {
        const id = url.searchParams.get("task_id");
        if (!id) return missing("task_id");
        return jsonResponse(200, { states: (await gateway.listForTask(id)).map(toGitStateDTO) });
      }
      if (method === "POST" && url.pathname === "/api/fetch_remote") {
        posted.push({ route: "fetch_remote", ...body });
        const { fetchedAt } = await gateway.fetchRemote(body.repository_id ?? "");
        return jsonResponse(200, { fetched_at: fetchedAt.toISOString() });
      }
      if (method === "POST" && url.pathname === "/api/push_branch") {
        posted.push({ route: "push_branch", ...body });
        const state = await gateway.push({ workspaceId: body.workspace_id ?? "", expectedHead: body.expected_head ?? "" });
        return jsonResponse(200, { state: toGitStateDTO(state) });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  // The project feed carries every kind of change; the gateway must pick out
  // the git states and ignore the rest.
  class StubEventSource {
    constructor(input) {
      const url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(url.searchParams.get("project_id") ?? "", (state) => this.send({ git_state: toGitStateDTO(state) }), () => {});
      queueMicrotask(() => {
        this.onopen?.({});
        this.send({ code_changed: { workspace_id: "ws1" } });
      });
    }
    send(dto) {
      this.onmessage?.({ data: typeof dto === "string" ? dto : JSON.stringify(dto) });
    }
    close() {
      sources.delete(this);
      this.unfollow();
    }
  }

  return {
    fetch,
    EventSource: StubEventSource,
    memory,
    posted,
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}

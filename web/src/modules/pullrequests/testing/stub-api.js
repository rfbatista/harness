// A stand-in for the harness server's pull request routes, backed by the
// memory gateway: a fetch and an EventSource that speak the Web UI contract's
// wire format. The real pullRequestsGateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { helloEnvelope, pageEventsProject, projectEnvelope } from "../../../shared/testing/page-events.js";
import { memoryPullRequests } from "../infrastructure/memory-gateway.js";
import { draftDTO, pullRequestDTO } from "./fixtures.js";

export function stubPullRequestsApi(world) {
  const memory = memoryPullRequests(world);
  const { gateway } = memory;
  const sources = new Set();
  /** The bodies the gateway posted, oldest first. */
  const posted = [];

  function fail(err) {
    const details = err.details?.pullRequest ? { pull_request: pullRequestDTO(err.details.pullRequest) } : err.details;
    return jsonResponse(err.status ?? 500, { error: err.message, code: codeOf(err), details });
  }

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const method = init.method ?? "GET";
    const body = init.body ? JSON.parse(init.body) : {};
    try {
      if (method === "GET" && url.pathname === "/api/pull_request") {
        const id = url.searchParams.get("workspace_id");
        if (!id) return jsonResponse(400, { error: "INVALID_INPUT: workspace_id is required", code: "INVALID_INPUT" });
        const { draft, pullRequest } = await gateway.get(id);
        return jsonResponse(200, { draft: draftDTO(draft), pull_request: pullRequest ? pullRequestDTO(pullRequest) : null });
      }
      if (method === "POST" && url.pathname === "/api/create_pull_request") {
        posted.push(body);
        const pr = await gateway.create({
          workspaceId: body.workspace_id ?? "",
          title: body.title ?? "",
          body: body.body ?? "",
          baseBranch: body.base_branch ?? "",
          draft: body.draft === true,
        });
        return jsonResponse(200, { pull_request: pullRequestDTO(pr) });
      }
      return jsonResponse(404, { error: `no route ${method} ${url.pathname}` });
    } catch (err) {
      return fail(err);
    }
  }

  // The page event stream carries every kind of change; the gateway must
  // pick out the pull requests and ignore the rest.
  class StubEventSource {
    constructor(input) {
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = gateway.follow(pageEventsProject(input) ?? "", (pr) => this.send(projectEnvelope({ pull_request: pullRequestDTO(pr) })), () => {});
      queueMicrotask(() => {
        this.onopen?.({});
        this.send(helloEnvelope());
        this.send(projectEnvelope({ ticket: { id: "t1", project_id: "p1", title: "Other news", status: "review" } }));
      });
    }
    send(envelope) {
      this.onmessage?.({ data: typeof envelope === "string" ? envelope : JSON.stringify(envelope) });
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
      for (const s of sources) s.send(typeof dto === "string" ? dto : projectEnvelope(dto));
    },
  };
}

// A stand-in for the server's repository agents routes and the page feed,
// backed by the memory gateway and speaking the real wire format, so the real
// gateway runs the contract suite against it.

import { codeOf } from "../../../shared/domain/errors.js";
import { jsonResponse } from "../../../shared/testing/doubles.js";
import { memoryRepositoryAgents } from "../infrastructure/memory-gateway.js";

const STATUS = {
  REPOSITORY_NOT_FOUND: 404,
  AGENT_NOT_FOUND: 404,
  REPOSITORY_AGENTS_CHANGED: 409,
  REPOSITORY_PROMPT_CHANGED: 409,
  REPOSITORY_PROMPT_TOO_LONG: 400,
};

const promptWire = (p) => ({ agent: p.agent, repository_id: p.repositoryId, content: p.content, version: p.version, limit: p.limit, unassigned: p.unassigned });

export function stubRepositoryAgentsApi(world) {
  const memory = memoryRepositoryAgents(world);
  const { gateway, wire } = memory;
  const sources = new Set();

  /** A decoded tab, back on the wire. */
  const tabWire = (tab) => wire.tab(wire.repo(tab.repository.id), tab.runningSessions === undefined ? {} : { running_sessions: tab.runningSessions });

  async function fetch(input, init = {}) {
    const url = new URL(input, "http://harness.test");
    const body = init.body ? JSON.parse(init.body) : {};
    const q = (k) => url.searchParams.get(k) ?? "";
    try {
      switch (`${init.method ?? "GET"} ${url.pathname}`) {
        case "GET /api/get_repository_agents":
          return jsonResponse(200, tabWire(await gateway.getRepositoryAgents(q("repository_id"))));
        case "POST /api/unassign_agent":
          return jsonResponse(200, tabWire(await gateway.unassignAgent({ repositoryId: body.repository_id, agent: body.agent, listVersion: body.list_version })));
        case "POST /api/assign_agent_again":
          return jsonResponse(200, tabWire(await gateway.assignAgentAgain({ repositoryId: body.repository_id, agent: body.agent, listVersion: body.list_version })));
        case "POST /api/assign_all_agents_again":
          return jsonResponse(200, tabWire(await gateway.assignAllAgentsAgain({ repositoryId: body.repository_id, listVersion: body.list_version })));
        case "GET /api/get_repository_prompt":
          return jsonResponse(200, promptWire(await gateway.getRepositoryPrompt(q("repository_id"), q("agent"))));
        case "POST /api/save_repository_prompt":
          return jsonResponse(200, promptWire(await gateway.saveRepositoryPrompt({ repositoryId: body.repository_id, agent: body.agent, content: body.content, version: body.version })));
        case "POST /api/preview_repository_prompt": {
          const p = await gateway.previewRepositoryPrompt({ repositoryId: body.repository_id, agent: body.agent, content: body.content });
          return jsonResponse(200, { agent_prompt: p.agentPrompt, repository_section: p.repositorySection, heading: p.heading });
        }
        default:
          return jsonResponse(404, { error: `no route ${url.pathname}` });
      }
    } catch (err) {
      return jsonResponse(STATUS[codeOf(err)] ?? 500, errorBody(err));
    }
  }

  function errorBody(err) {
    const out = { error: err.message, code: codeOf(err) };
    const d = err.details;
    if (!d) return out;
    if (d.current?.repository) out.current = tabWire(d.current);
    else if (d.current) out.current = promptWire(d.current);
    if (d.limit !== undefined) Object.assign(out, { limit: d.limit, length: d.length });
    return out;
  }

  // GET /api/project_events: one change per message, as the server sends it.
  class StubEventSource {
    constructor(input) {
      this.url = new URL(input, "http://harness.test");
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      sources.add(this);
      this.unfollow = this.url.pathname === "/api/project_events"
        ? gateway.followChanges((c) => this.send({ repository_agents_changed: { project_id: c.projectId, repository_id: c.repositoryId, agents: c.agents } }))
        : () => {};
      queueMicrotask(() => this.onopen?.({}));
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
    pushRaw(dto) {
      for (const s of sources) s.send(dto);
    },
  };
}

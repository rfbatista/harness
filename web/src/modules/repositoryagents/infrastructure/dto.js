// The wire format of the repository agents contract and its mapping to camelCase.

import { Codes, StructuredError } from "../../../shared/domain/errors.js";

const bad = (why) => {
  throw new StructuredError(Codes.BAD_RESPONSE, `Unexpected repository agents answer: ${why}.`);
};
const text = (v) => (typeof v === "string" ? v : "");
const count = (v) => (Number.isInteger(v) && v >= 0 ? v : 0);

const toPrompt = (dto) =>
  Object.freeze({ set: dto?.set === true, summary: text(dto?.summary), length: count(dto?.length), version: text(dto?.version) });

const toAgentRow = (dto) => {
  if (!dto || typeof dto.name !== "string" || dto.name === "") bad("an agent without a name");
  return Object.freeze({
    name: dto.name,
    description: text(dto.description),
    state: dto.state === "unassigned" || dto.state === "not_enabled" ? dto.state : "assigned",
    unassigned: dto.unassigned === true,
    prompt: toPrompt(dto.prompt),
    runningSessions: count(dto.running_sessions),
    previousName: text(dto.previous_name),
  });
};

/** GET /api/get_repository_agents, and the answer of every list write. */
export function toRepositoryAgents(body) {
  if (!body || typeof body !== "object") bad("expected an object");
  if (!body.repository || typeof body.repository.id !== "string") bad("expected {repository}");
  if (body.agents !== null && body.agents !== undefined && !Array.isArray(body.agents)) bad("expected agents: [...]");
  const p = body.project ?? {};
  return Object.freeze({
    repository: Object.freeze({ id: body.repository.id, name: text(body.repository.name) }),
    project: Object.freeze({
      id: text(p.id),
      name: text(p.name),
      agentsMode: p.agents_mode === "selected" ? "selected" : "all",
      enabledCount: count(p.enabled_count),
      catalogCount: count(p.catalog_count),
    }),
    listVersion: text(body.list_version),
    agents: Object.freeze((body.agents ?? []).map(toAgentRow)),
    ...(body.running_sessions === undefined ? {} : { runningSessions: count(body.running_sessions) }),
  });
}

/** GET /api/get_repository_prompt, and the answer of save_repository_prompt. */
export function toRepositoryPrompt(body) {
  if (!body || typeof body.agent !== "string" || typeof body.repository_id !== "string") bad("expected {agent, repository_id}");
  return Object.freeze({
    agent: body.agent,
    repositoryId: body.repository_id,
    content: text(body.content),
    version: text(body.version),
    limit: Number.isInteger(body.limit) ? body.limit : 8000,
    unassigned: body.unassigned === true,
  });
}

/** POST /api/preview_repository_prompt */
export function toPreview(body) {
  if (!body || typeof body.agent_prompt !== "string") bad("expected {agent_prompt}");
  return Object.freeze({ agentPrompt: body.agent_prompt, repositorySection: text(body.repository_section), heading: text(body.heading) });
}

/** One page-feed message, as a change; null for any other message. */
export function toAgentsChange(data) {
  const body = data && typeof data === "object" ? data.repository_agents_changed : undefined;
  if (!body || typeof body !== "object") return null;
  if (typeof body.repository_id !== "string" || body.repository_id === "") return null;
  const agents = Array.isArray(body.agents) ? body.agents.filter((a) => typeof a === "string") : [];
  return Object.freeze({ kind: "changed", projectId: text(body.project_id), repositoryId: body.repository_id, agents: Object.freeze(agents) });
}

/**
 * A refusal with its details decoded: the current state of a stale write, the
 * limit and length of a too long prompt. Any other error is returned as is.
 */
export function withDetails(err) {
  if (!(err instanceof StructuredError)) return err;
  const d = err.details ?? {};
  switch (err.code) {
    case Codes.REPOSITORY_AGENTS_CHANGED:
      return new StructuredError(err.code, err.message, err.status, { current: toRepositoryAgents(d.current) });
    case Codes.REPOSITORY_PROMPT_CHANGED:
      return new StructuredError(err.code, err.message, err.status, { current: toRepositoryPrompt(d.current) });
    case Codes.REPOSITORY_PROMPT_TOO_LONG:
      return new StructuredError(err.code, err.message, err.status, { limit: count(d.limit), length: count(d.length) });
    case Codes.AGENT_NOT_ASSIGNED:
      return new StructuredError(err.code, err.message, err.status, {
        agent: text(d.agent),
        repositoryId: text(d.repository_id),
        repositoryName: text(d.repository_name),
        assigned: Array.isArray(d.assigned) ? d.assigned.filter((a) => typeof a === "string") : [],
      });
    default:
      return err;
  }
}

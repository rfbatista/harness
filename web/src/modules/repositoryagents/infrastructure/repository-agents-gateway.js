// RepositoryAgentsGateway over the harness HTTP API (RPC-style POSTs) and the
// page feed (/api/project_events, server-sent events).

import { toAgentsChange, toPreview, toRepositoryAgents, toRepositoryPrompt, withDetails } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} events
 * @returns {import("../domain/ports.js").RepositoryAgentsGateway}
 */
export function repositoryAgentsGateway(api, events) {
  const call = async (run) => {
    try {
      return await run();
    } catch (err) {
      throw withDetails(err);
    }
  };
  const write = (path, body, decode) => call(async () => decode(await api.post(path, body)));

  return {
    decodeRepositoryAgents: toRepositoryAgents,
    decodeRepositoryPrompt: toRepositoryPrompt,

    getRepositoryAgents: (repositoryId) => call(async () => toRepositoryAgents(await api.get("/get_repository_agents", { repository_id: repositoryId }))),

    unassignAgent: ({ repositoryId, agent, listVersion }) =>
      write("/unassign_agent", { repository_id: repositoryId, agent, list_version: listVersion }, toRepositoryAgents),

    assignAgentAgain: ({ repositoryId, agent, listVersion }) =>
      write("/assign_agent_again", { repository_id: repositoryId, agent, list_version: listVersion }, toRepositoryAgents),

    assignAllAgentsAgain: ({ repositoryId, listVersion }) =>
      write("/assign_all_agents_again", { repository_id: repositoryId, list_version: listVersion }, toRepositoryAgents),

    getRepositoryPrompt: (repositoryId, agent) =>
      call(async () => toRepositoryPrompt(await api.get("/get_repository_prompt", { repository_id: repositoryId, agent }))),

    saveRepositoryPrompt: ({ repositoryId, agent, content, version }) =>
      write("/save_repository_prompt", { repository_id: repositoryId, agent, content, version }, toRepositoryPrompt),

    previewRepositoryPrompt: ({ repositoryId, agent, content }) =>
      write("/preview_repository_prompt", { repository_id: repositoryId, agent, content }, toPreview),

    followChanges(onChange, onStatus = () => {}) {
      return events.follow("/project_events", {
        onMessage(data) {
          const change = toAgentsChange(data);
          if (change) onChange(change);
        },
        onStatus,
      });
    },
  };
}

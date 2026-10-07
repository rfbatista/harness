// SessionGateway over the harness HTTP API and its SSE feed.

import { toBranches, toChange, toCreated, toResumeBody, toSeed, toSessionList, toStartBody } from "./dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").SessionGateway}
 */
export function sessionsGateway(api, feed) {
  return {
    decodeSeed: toSeed,

    async list({ projectId, ticketId }, signal) {
      return toSessionList(await api.get("/sessions", { project_id: projectId, ticket_id: ticketId }, signal));
    },

    async start(request) {
      return toCreated(await api.post("/start_interactive_session", toStartBody(request)));
    },

    async resume(sessionId, size) {
      return toCreated(await api.post("/resume_interactive_session", toResumeBody(sessionId, size)));
    },

    async listBranches(repositoryId) {
      return toBranches(await api.get("/list_branches", { repository_id: repositoryId }));
    },

    async stop(sessionId) {
      await api.post(`/sessions/${encodeURIComponent(sessionId)}/stop`);
    },

    async remove(sessionId) {
      await api.del(`/sessions/${encodeURIComponent(sessionId)}`);
    },

    follow(projectId, onChange, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          try {
            onChange(toChange(dto));
          } catch {
            // A change we cannot read is dropped; the next resync corrects the list.
          }
        },
        onStatus,
      });
    },
  };
}

// ArtifactGateway over the harness HTTP API: the artifact list route, the
// scope move, attach and detach, the delete, the `artifact` events on a
// session's SSE stream (the one interactive sessions already use for status
// and done) and the `artifact` changes on the project feed.

import { toArtifactAnswer, toArtifactEvent, toArtifactList, toProjectArtifactChange } from "./artifact-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {string} [base]  the API prefix the browser loads artifact bytes from
 * @returns {import("../domain/ports.js").ArtifactGateway}
 */
export function artifactsGateway(api, feed, base = "/api") {
  return {
    async list(sessionId, signal) {
      return toArtifactList(await api.get("/artifacts", { session_id: sessionId }, signal), base);
    },

    async listProject(projectId, signal) {
      return toArtifactList(await api.get("/artifacts", { project_id: projectId, scope: "project" }, signal), base);
    },

    decodeArtifacts(rows) {
      return toArtifactList({ artifacts: rows }, base);
    },

    async listTask(ticketId, signal) {
      return toArtifactList(await api.get("/artifacts", { ticket_id: ticketId }, signal), base);
    },

    async attach(artifactId, ticketId) {
      return toArtifactAnswer(await api.post("/attach_artifact_to_ticket", { artifact_id: artifactId, ticket_id: ticketId }), base);
    },

    async detach(artifactId, ticketId) {
      return toArtifactAnswer(await api.post("/detach_artifact_from_ticket", { artifact_id: artifactId, ticket_id: ticketId }), base);
    },

    async setScope(artifactId, scope) {
      return toArtifactAnswer(await api.post("/set_artifact_scope", { artifact_id: artifactId, scope }), base);
    },

    async remove(artifactId) {
      await api.del(`/artifacts/${encodeURIComponent(artifactId)}`);
    },

    follow(sessionId, onEvent, onStatus) {
      return feed.follow(`/sessions/${encodeURIComponent(sessionId)}/events`, {
        onMessage: (dto) => {
          let event;
          try {
            event = toArtifactEvent(dto, base);
          } catch {
            return; // an event we cannot read is dropped; the next resync corrects the list
          }
          if (event) onEvent(event);
        },
        onStatus,
      });
    },

    followProject(projectId, onChange, onStatus) {
      return feed.follow(`/events?project_id=${encodeURIComponent(projectId)}`, {
        onMessage: (dto) => {
          let change;
          try {
            change = toProjectArtifactChange(dto, base);
          } catch {
            return; // a change we cannot read is dropped; the next resync corrects the list
          }
          if (change) onChange(change);
        },
        onStatus,
      });
    },
  };
}

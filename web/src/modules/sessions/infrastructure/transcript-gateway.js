// TranscriptGateway over the harness HTTP API: GET /api/sessions/{id}/transcript.

import { toTranscriptPage } from "./transcript-dto.js";

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @returns {import("../domain/ports.js").TranscriptGateway}
 */
export function transcriptGateway(api) {
  return {
    async page(sessionId, { before, after, limit } = {}, signal) {
      const query = { before, after, limit: limit === undefined ? undefined : String(limit) };
      return toTranscriptPage(await api.get(`/sessions/${encodeURIComponent(sessionId)}/transcript`, query, signal));
    },
  };
}

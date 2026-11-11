// TranscriptGateway over the harness HTTP API: GET /api/sessions/{id}/transcript,
// and the session's event log (GET /api/sessions/{id}/history) for the lines a
// restart adds to the History tab.

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
    async events(sessionId, { afterSeq = 0 } = {}, signal) {
      const body = await api.get(`/sessions/${encodeURIComponent(sessionId)}/history`, { from_seq: String(afterSeq) }, signal);
      const list = Array.isArray(body?.events) ? body.events : [];
      return { events: list.map((e) => ({ seq: e.seq, type: e.type, at: e.at, payload: e })), lastSeq: list.reduce((n, e) => Math.max(n, Number(e.seq) || 0), afterSeq) };
    },
  };
}

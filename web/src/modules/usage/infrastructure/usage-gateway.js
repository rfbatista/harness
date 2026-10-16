// UsageGateway over the harness HTTP API: /api/projects/{p}/usage, its
// records, its usage records as the page event stream carries them (kind
// usage, one record each), and /api/usage/totals for one task or session.

import { toRecord, toRecords, toSeed, toSummary, toTotals, toTotalsSeed } from "./dto.js";

const base = (projectId) => `/projects/${encodeURIComponent(projectId)}/usage`;

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").UsageGateway}
 */
export function usageGateway(api, feed) {
  return {
    decodeSeed: toSeed,
    decodeTotalsSeed: toTotalsSeed,

    async totals({ taskId = "", sessionId = "" }, signal) {
      return toTotals(await api.get("/usage/totals", { task_id: taskId, session_id: sessionId }, signal));
    },

    async summary(projectId, range, signal) {
      return toSummary(await api.get(base(projectId), { range }, signal));
    },

    async recent(projectId, signal) {
      return toRecords(await api.get(`${base(projectId)}/records`, {}, signal));
    },

    follow(projectId, onRecord, onStatus) {
      return feed.followPage({ kind: "usage", projectId }, {
        onMessage: (dto) => {
          let record;
          try {
            record = toRecord(dto);
          } catch {
            return; // a record we cannot read is dropped; the next resync corrects the page
          }
          onRecord(record);
        },
        onStatus,
      });
    },
  };
}

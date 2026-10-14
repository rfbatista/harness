// UsageGateway over the harness HTTP API: /api/projects/{p}/usage, its
// records and its event stream (bare `data:` lines, one record each).

import { toRecord, toRecords, toSeed, toSummary } from "./dto.js";

const base = (projectId) => `/projects/${encodeURIComponent(projectId)}/usage`;

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @returns {import("../domain/ports.js").UsageGateway}
 */
export function usageGateway(api, feed) {
  return {
    decodeSeed: toSeed,

    async summary(projectId, range, signal) {
      return toSummary(await api.get(base(projectId), { range }, signal));
    },

    async recent(projectId, signal) {
      return toRecords(await api.get(`${base(projectId)}/records`, {}, signal));
    },

    follow(projectId, onRecord, onStatus) {
      return feed.follow(`${base(projectId)}/events`, {
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

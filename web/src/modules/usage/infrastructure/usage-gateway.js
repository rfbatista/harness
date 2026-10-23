// UsageGateway over the harness HTTP API: /api/projects/{p}/usage, its
// records and its event stream (bare `data:` lines, one record each), and
// /api/usage/totals for one task or session. The live call stream is not a
// feed: it names its events, so it gets an EventSource of its own per viewer.

import { FeedStatus } from "../../../shared/domain/feed.js";
import { toRecord, toRecords, toSeed, toSummary, toTotals, toTotalsSeed } from "./dto.js";

const LIVE_EVENTS = ["ready", "call.started", "call.finished", "calls.missed"];

const base = (projectId) => `/projects/${encodeURIComponent(projectId)}/usage`;

/**
 * @param {import("../../../shared/infrastructure/api.js").ApiClient} api
 * @param {import("../../../shared/infrastructure/feed.js").Feed} feed
 * @param {{ EventSource?: typeof globalThis.EventSource, liveBase?: string }} [options]
 * @returns {import("../domain/ports.js").UsageGateway}
 */
export function usageGateway(api, feed, { EventSource = globalThis.EventSource, liveBase = "/api" } = {}) {
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

    openLive(projectId, { onEvent, onStatus }) {
      let source = new EventSource(`${liveBase}${base(projectId)}/live`);
      onStatus(FeedStatus.CONNECTING);
      for (const type of LIVE_EVENTS) {
        source.addEventListener(type, (e) => {
          if (!source) return;
          let data;
          try {
            data = JSON.parse(e.data);
          } catch {
            return; // an event we cannot read is dropped; the stream goes on
          }
          if (type === "ready") onStatus(FeedStatus.LIVE);
          else onEvent(type, data);
        });
      }
      // The browser retries a dropped stream by itself; `ready` follows when it is back.
      source.onerror = () => source && onStatus(FeedStatus.PAUSED);
      return () => {
        source?.close();
        source = null;
      };
    },
  };
}

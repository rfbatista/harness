// Server-sent events with the same recovery as the TUI's live.Follower: a
// dropped stream reports `paused`, retries with backoff (1s doubling to 30s),
// and reports `resynced` on reconnecting, because changes were missed.

import { FeedStatus } from "../domain/feed.js";

const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 30_000;

/**
 * @typedef {object} Feed
 * @property {(path: string, handlers: {
 *   onMessage: (data: any) => void,
 *   onStatus?: (status: import("../domain/feed.js").FeedStatus) => void,
 * }) => () => void} follow  Opens the stream; returns the function that closes it.
 */

/**
 * @param {{
 *   base: string,
 *   EventSource?: typeof globalThis.EventSource,
 *   setTimeout?: typeof globalThis.setTimeout,
 *   clearTimeout?: typeof globalThis.clearTimeout,
 * }} options
 * @returns {Feed}
 */
export function feed({
  base,
  EventSource = globalThis.EventSource,
  setTimeout = globalThis.setTimeout.bind(globalThis),
  clearTimeout = globalThis.clearTimeout.bind(globalThis),
}) {
  return {
    follow(path, { onMessage, onStatus = () => {} }) {
      let source = null;
      let timer = null;
      let delay = FIRST_RETRY_MS;
      let dropped = false;
      let closed = false;

      function open() {
        source = new EventSource(base + path);
        source.onopen = () => {
          delay = FIRST_RETRY_MS;
          onStatus(dropped ? FeedStatus.RESYNCED : FeedStatus.LIVE);
          if (dropped) onStatus(FeedStatus.LIVE);
          dropped = false;
        };
        source.onmessage = (event) => {
          let data;
          try {
            data = JSON.parse(event.data);
          } catch {
            return; // a malformed line is skipped, not fatal
          }
          onMessage(data);
        };
        source.onerror = () => {
          // EventSource's own reconnect has no backoff; take over.
          source.close();
          if (closed) return;
          dropped = true;
          onStatus(FeedStatus.PAUSED);
          timer = setTimeout(open, delay);
          delay = Math.min(delay * 2, MAX_RETRY_MS);
        };
      }

      onStatus(FeedStatus.CONNECTING);
      open();

      return () => {
        closed = true;
        clearTimeout(timer);
        source?.close();
      };
    },
  };
}

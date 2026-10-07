// Server-sent events with the same recovery as the TUI's live.Follower: a
// dropped stream reports `paused`, retries with backoff (1s doubling to 30s),
// and reports `resynced` on reconnecting, because changes were missed.
//
// Followers of the same path share one stream. A page follows the project
// feed from several components (the rail, the sessions page, the reviews
// band, …), and the browser allows only a few connections per host across
// all its tabs. The stream opens with its first follower and closes with its
// last; one that joins late is told the stream's status at once.

import { FeedStatus } from "../domain/feed.js";

const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 30_000;

/**
 * @typedef {object} Feed
 * @property {(path: string, handlers: {
 *   onMessage: (data: any) => void,
 *   onStatus?: (status: import("../domain/feed.js").FeedStatus) => void,
 * }) => () => void} follow  Follows the stream; returns the function that stops following it.
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
  /** @type {Map<string, ReturnType<typeof stream>>} */
  const streams = new Map();

  /** One EventSource and the followers it fans out to. */
  function stream(path) {
    const followers = new Set();
    let source = null;
    let timer = null;
    let delay = FIRST_RETRY_MS;
    let dropped = false;
    let status = FeedStatus.CONNECTING;

    // A follower that throws must not keep the others from hearing.
    function each(fn) {
      for (const f of [...followers]) {
        try {
          fn(f);
        } catch {
          // its own problem; the stream goes on
        }
      }
    }
    function report(next) {
      status = next;
      each((f) => f.onStatus(next));
    }

    function open() {
      source = new EventSource(base + path);
      source.onopen = () => {
        delay = FIRST_RETRY_MS;
        report(dropped ? FeedStatus.RESYNCED : FeedStatus.LIVE);
        if (dropped) report(FeedStatus.LIVE);
        dropped = false;
      };
      source.onmessage = (event) => {
        let data;
        try {
          data = JSON.parse(event.data);
        } catch {
          return; // a malformed line is skipped, not fatal
        }
        each((f) => f.onMessage(data));
      };
      source.onerror = () => {
        // EventSource's own reconnect has no backoff; take over.
        source.close();
        if (followers.size === 0) return;
        dropped = true;
        report(FeedStatus.PAUSED);
        timer = setTimeout(open, delay);
        delay = Math.min(delay * 2, MAX_RETRY_MS);
      };
    }

    return {
      add(follower) {
        followers.add(follower);
        if (followers.size === 1) {
          report(FeedStatus.CONNECTING);
          open();
        } else {
          follower.onStatus(status);
        }
      },
      /** @returns {boolean} the stream has no followers left and is closed */
      remove(follower) {
        followers.delete(follower);
        if (followers.size > 0) return false;
        clearTimeout(timer);
        source?.close();
        return true;
      },
    };
  }

  return {
    follow(path, { onMessage, onStatus = () => {} }) {
      let s = streams.get(path);
      if (!s) {
        s = stream(path);
        streams.set(path, s);
      }
      const follower = { onMessage, onStatus };
      s.add(follower);

      let following = true;
      return () => {
        if (!following) return;
        following = false;
        if (s.remove(follower)) streams.delete(path);
      };
    },
  };
}

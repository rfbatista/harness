// The tab's one live stream: every feed the page follows rides a single
// EventSource on /api/stream, added and dropped with POST /api/stream/follow
// and /api/stream/unfollow. Over plain HTTP the browser allows six
// connections per host across all its tabs; with an EventSource per feed
// (the project, the project list, the inbox, a session, usage) two tabs used
// them all and every other request waited behind them.
//
// It keeps feed.js's contract and recovery: followers of a path share it, a
// dropped feed reports `paused` and is followed again with backoff (1s
// doubling to 30s), and reports `resynced` when it is back, because changes
// were missed. A drop of the whole stream drops every feed on it.

import { FeedStatus } from "../domain/feed.js";

const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 30_000;

/**
 * @param {{
 *   base: string,
 *   api: import("./api.js").ApiClient,
 *   EventSource?: typeof globalThis.EventSource,
 *   setTimeout?: typeof globalThis.setTimeout,
 *   clearTimeout?: typeof globalThis.clearTimeout,
 * }} options
 * @returns {import("./feed.js").Feed}
 */
export function streamFeed({
  base,
  api,
  EventSource = globalThis.EventSource,
  setTimeout = globalThis.setTimeout.bind(globalThis),
  clearTimeout = globalThis.clearTimeout.bind(globalThis),
}) {
  /** @type {Map<string, ReturnType<typeof topic>>} */
  const topics = new Map();
  let source = null;
  let streamId = null; // set once the stream's first message names it
  let timer = null;
  let delay = FIRST_RETRY_MS;

  /** One feed on the stream and the followers it fans out to. */
  function topic(path) {
    const followers = new Set();
    let status = FeedStatus.CONNECTING;
    let dropped = false;
    let timer = null;
    let delay = FIRST_RETRY_MS;

    // A follower that throws must not keep the others from hearing.
    function each(fn) {
      for (const f of [...followers]) {
        try {
          fn(f);
        } catch {
          // its own problem; the feed goes on
        }
      }
    }
    function report(next) {
      status = next;
      each((f) => f.onStatus(next));
    }
    const current = () => topics.get(path) === t;

    function subscribe() {
      const id = streamId;
      api.post("/stream/follow", { stream: id, path }).then(
        () => {
          if (!current() || streamId !== id) return;
          delay = FIRST_RETRY_MS;
          if (dropped) report(FeedStatus.RESYNCED);
          report(FeedStatus.LIVE);
          dropped = false;
        },
        () => {
          if (current() && streamId === id) retry();
        },
      );
    }

    function retry() {
      dropped = true;
      report(FeedStatus.PAUSED);
      clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        if (current() && streamId) subscribe();
      }, delay);
      delay = Math.min(delay * 2, MAX_RETRY_MS);
    }

    const t = {
      followers,
      get status() {
        return status;
      },
      subscribe,
      retry,
      deliver(data) {
        each((f) => f.onMessage(data));
      },
      /** The whole stream dropped: wait for it to come back. */
      drop() {
        clearTimeout(timer);
        timer = null;
        delay = FIRST_RETRY_MS;
        dropped = true;
        report(FeedStatus.PAUSED);
      },
      report,
      stop() {
        clearTimeout(timer);
      },
    };
    return t;
  }

  function open() {
    timer = null;
    streamId = null;
    source = new EventSource(base + "/stream");
    source.onmessage = (event) => {
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch {
        return; // a malformed line is skipped, not fatal
      }
      if (msg.stream) {
        streamId = msg.stream;
        delay = FIRST_RETRY_MS;
        for (const t of topics.values()) t.subscribe();
        return;
      }
      const t = topics.get(msg.path);
      if (!t) return;
      if (msg.end) t.retry();
      else t.deliver(msg.data);
    };
    source.onerror = () => {
      // EventSource's own reconnect has no backoff and would need following
      // every feed again anyway; take over.
      source.close();
      source = null;
      streamId = null;
      if (topics.size === 0) return;
      for (const t of topics.values()) t.drop();
      timer = setTimeout(open, delay);
      delay = Math.min(delay * 2, MAX_RETRY_MS);
    };
  }

  function close() {
    clearTimeout(timer);
    timer = null;
    source?.close();
    source = null;
    streamId = null;
    delay = FIRST_RETRY_MS;
  }

  return {
    follow(path, { onMessage, onStatus = () => {} }) {
      let t = topics.get(path);
      const follower = { onMessage, onStatus };
      if (!t) {
        t = topic(path);
        topics.set(path, t);
        t.followers.add(follower);
        t.report(FeedStatus.CONNECTING);
        if (streamId) t.subscribe();
        else if (!source && !timer) open();
      } else {
        t.followers.add(follower);
        follower.onStatus(t.status);
      }

      let following = true;
      return () => {
        if (!following) return;
        following = false;
        t.followers.delete(follower);
        if (t.followers.size > 0) return;
        t.stop();
        topics.delete(path);
        if (topics.size === 0) close();
        else if (streamId) api.post("/stream/unfollow", { stream: streamId, path }).catch(() => {});
      };
    },
  };
}

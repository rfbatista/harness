// The tab's one live stream: every feed the page follows rides a single
// EventSource on /api/stream, added and dropped with POST /api/stream/follow
// and /api/stream/unfollow. Ported from the Alpine client's stream-feed.js,
// whose protocol it keeps. A dropped feed reports `paused` and is followed
// again with backoff (1 s doubling to 30 s), and reports `resynced` when it is
// back, because changes were missed.

import type { Feed, FeedHandlers, FeedStatus } from "../kernel/feed";
import type { ApiClient } from "./api-client";
import { FollowRequestDto, StreamMessageDto } from "./dto/stream-dto";

const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 30_000;

export interface Timers {
  setTimeout(fn: () => void, ms: number): unknown;
  clearTimeout(id: unknown): void;
}

export const systemTimers: Timers = {
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
  clearTimeout: (id) => globalThis.clearTimeout(id as number),
};

export interface StreamFeedOptions extends Partial<Timers> {
  base: string;
  api: ApiClient;
  EventSource?: typeof globalThis.EventSource;
}

type Follower = Required<FeedHandlers>;

interface Topic {
  readonly followers: Set<Follower>;
  readonly status: FeedStatus;
  subscribe(): void;
  retry(): void;
  deliver(data: unknown): void;
  drop(): void;
  report(status: FeedStatus): void;
  stop(): void;
}

export function streamFeed({ base, api, EventSource = globalThis.EventSource, setTimeout = systemTimers.setTimeout, clearTimeout = systemTimers.clearTimeout }: StreamFeedOptions): Feed {
  const topics = new Map<string, Topic>();
  let source: EventSource | null = null;
  let streamId: string | null = null; // set once the stream's first message names it
  let timer: unknown = null;
  let delay = FIRST_RETRY_MS;

  /** One feed on the stream and the followers it fans out to. */
  function topic(path: string): Topic {
    const followers = new Set<Follower>();
    let status: FeedStatus = "connecting";
    let dropped = false;
    let topicTimer: unknown = null;
    let topicDelay = FIRST_RETRY_MS;

    // A follower that throws must not keep the others from hearing.
    const each = (fn: (follower: Follower) => void) => {
      for (const follower of [...followers]) {
        try {
          fn(follower);
        } catch {
          // its own problem; the feed goes on
        }
      }
    };
    const report = (next: FeedStatus) => {
      status = next;
      each((f) => f.onStatus(next));
    };
    const current = () => topics.get(path) === t;

    function subscribe() {
      const id = streamId;
      if (id === null) return;
      api.post("/stream/follow", new FollowRequestDto(id, path)).then(
        () => {
          if (!current() || streamId !== id) return;
          topicDelay = FIRST_RETRY_MS;
          if (dropped) report("resynced");
          report("live");
          dropped = false;
        },
        () => {
          if (current() && streamId === id) retry();
        },
      );
    }

    function retry() {
      dropped = true;
      report("paused");
      clearTimeout(topicTimer);
      topicTimer = setTimeout(() => {
        topicTimer = null;
        if (current() && streamId) subscribe();
      }, topicDelay);
      topicDelay = Math.min(topicDelay * 2, MAX_RETRY_MS);
    }

    const t: Topic = {
      followers,
      get status() {
        return status;
      },
      subscribe,
      retry,
      deliver: (data) => each((f) => f.onMessage(data)),
      /** The whole stream dropped: wait for it to come back. */
      drop() {
        clearTimeout(topicTimer);
        topicTimer = null;
        topicDelay = FIRST_RETRY_MS;
        dropped = true;
        report("paused");
      },
      report,
      stop: () => clearTimeout(topicTimer),
    };
    return t;
  }

  function open() {
    timer = null;
    streamId = null;
    const opened = new EventSource(base + "/stream");
    source = opened;
    opened.onmessage = (event) => {
      let msg: StreamMessageDto;
      try {
        msg = StreamMessageDto.fromJSON(JSON.parse(event.data as string));
      } catch {
        return; // a malformed line is skipped, not fatal
      }
      if (msg.stream !== undefined) {
        streamId = msg.stream;
        delay = FIRST_RETRY_MS;
        for (const t of topics.values()) t.subscribe();
        return;
      }
      const t = msg.path === undefined ? undefined : topics.get(msg.path);
      if (!t) return;
      if (msg.end) t.retry();
      else t.deliver(msg.data);
    };
    opened.onerror = () => {
      // EventSource's own reconnect has no backoff and would need following
      // every feed again anyway; take over.
      opened.close();
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
      const follower: Follower = { onMessage, onStatus };
      let t = topics.get(path);
      if (!t) {
        t = topic(path);
        topics.set(path, t);
        t.followers.add(follower);
        t.report("connecting");
        if (streamId) t.subscribe();
        else if (!source && !timer) open();
      } else {
        t.followers.add(follower);
        follower.onStatus(t.status);
      }

      const joined = t;
      let following = true;
      return () => {
        if (!following) return;
        following = false;
        joined.followers.delete(follower);
        if (joined.followers.size > 0) return;
        joined.stop();
        topics.delete(path);
        if (topics.size === 0) close();
        else if (streamId) api.post("/stream/unfollow", new FollowRequestDto(streamId, path)).catch(() => {});
      };
    },
  };
}

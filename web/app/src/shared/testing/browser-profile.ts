// Tabs of one browser profile, for the shared stream's protocol tests. Built on
// the Alpine client's cross-tab doubles (Web Locks, BroadcastChannel, EventSource),
// so the same simulated browser runs this client's adapter and the Alpine client's.

import assert from "node:assert/strict";
import { fakeCrossTab, fakeEventSourceClass, fakeStreamApi, manualTimers } from "../../../../src/shared/testing/doubles.js";
import { sharedFeed as alpineSharedFeed } from "../../../../src/shared/infrastructure/shared-feed.js";
import type { Feed, FeedStatus } from "../kernel/feed";
import { sharedFeed } from "../infrastructure/shared-feed";
import type { ApiClient } from "../infrastructure/api-client";

export interface Follower {
  statuses: FeedStatus[];
  messages: unknown[];
  close(): void;
}

export interface Tab {
  feed: Feed;
  follow(path: string): Follower;
  /** The EventSources this tab opened. */
  streams: { closed: boolean; url: string; message(data: unknown): void }[];
  close(options?: { crash?: boolean }): void;
  freeze(): void;
  thaw(): void;
  fire(type: string, event?: object): void;
}

export const flush = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

/** "react" is this client's adapter; "alpine" is the Alpine client's shared-feed.js, the other party of the protocol. */
export type Client = "react" | "alpine";

export function browserProfile() {
  const cross = fakeCrossTab();
  const es = fakeEventSourceClass();
  const timers = manualTimers();
  const { api, posts } = fakeStreamApi() as { api: ApiClient & { refuse: boolean }; posts: Record<string, unknown>[] };
  // One clock and one heartbeat for all tabs: beat() moves the clock on and runs every tab's interval once.
  let clock = 0;
  const intervals = new Map<number, () => void>();
  let nextInterval = 1;
  const heartbeat = {
    now: () => clock,
    setInterval(fn: () => void) {
      const id = nextInterval++;
      intervals.set(id, fn);
      return id;
    },
    clearInterval: (id: unknown) => void intervals.delete(id as number),
  };
  const beat = (ms: number) => {
    clock += ms;
    for (const fn of [...intervals.values()]) fn();
  };
  const later = (ms: number) => {
    clock += ms;
  };

  const tabs: Tab[] = [];
  function open(client: Client = "react"): Tab {
    const t = cross.tab();
    const mine = fakeEventSourceClass();
    // Each tab's EventSources are counted in `es` too, and close with the tab.
    class TabEventSource extends mine.EventSource {
      constructor(url: string) {
        super(url);
        es.instances.push(this);
      }
    }
    let frozen = false;
    const options = {
      base: "/api",
      api,
      EventSource: TabEventSource,
      ...timers,
      locks: t.locks,
      BroadcastChannel: t.BroadcastChannel,
      window: t.window,
      now: heartbeat.now,
      // A frozen tab's interval does not run.
      setInterval: (fn: () => void) =>
        heartbeat.setInterval(() => {
          if (!frozen) fn();
        }),
      clearInterval: heartbeat.clearInterval,
    };
    const feed: Feed = client === "react" ? sharedFeed(options as never) : (alpineSharedFeed({ ...options, api: api as never } as never) as Feed);
    const tab: Tab = {
      feed,
      follow(path) {
        const statuses: FeedStatus[] = [];
        const messages: unknown[] = [];
        const close = feed.follow(path, { onMessage: (m) => messages.push(m), onStatus: (s) => statuses.push(s) });
        return { statuses, messages, close };
      },
      close(opts) {
        t.close(opts);
        for (const s of mine.instances) s.closed = true;
      },
      streams: mine.instances,
      fire: (type, event) => t.fire(type, event),
      freeze() {
        frozen = true;
        t.freeze();
      },
      thaw() {
        frozen = false;
        t.thaw();
      },
    };
    tabs.push(tab);
    return tab;
  }
  const openStreams = () => es.instances.filter((s: { closed: boolean }) => !s.closed);
  const stream = () => {
    const live = openStreams();
    assert.equal(live.length, 1, "exactly one open stream");
    return live[0];
  };
  const hello = (id = "st1") => stream().message({ stream: id });
  return { open, tabs, es, timers, api, posts, openStreams, stream, hello, beat, later };
}

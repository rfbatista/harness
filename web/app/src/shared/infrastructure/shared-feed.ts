// The page event stream, shared by every harness tab of a browser profile
// (ADR 0011). A TypeScript port of the Alpine client's shared-feed.js, which it
// interoperates with: the same Web Lock and BroadcastChannel names and the same
// messages, so the Alpine client, this one, and every other tab keep one stream
// between them, whichever holds it.
//
// One tab, the stream-holding tab, holds a Web Lock and with it the only
// stream (a stream-feed). The browser releases the lock when that tab closes
// or crashes, and the next tab waiting for it takes over. Tabs talk over a
// BroadcastChannel: each tells the stream-holding tab what it follows, and it
// follows the union on the server, counting followers per path across tabs, and
// relays every event and status to the tabs. A BroadcastChannel also reaches
// the other channels of the same tab, so the Alpine client in this tab is one
// more tab to it.
//
// A hung stream-holding tab keeps its lock, so the others check that it still
// answers: each pings it, and one that hears nothing for SILENCE_MS takes the
// lock from it. A browser without Web Locks or BroadcastChannel gets a stream
// per tab.

import type { Feed, FeedHandlers, FeedStatus } from "../kernel/feed";
import type { ApiClient } from "./api-client";
import { streamFeed, systemTimers, type Timers } from "./stream-feed";

/** The lock's and the channel's name: the Alpine client's, so both interoperate. */
export const NAME = "harness-page-event-stream";
const PING_MS = 2_000;
const SILENCE_MS = 10_000;
const FIRST_HANDOVER_DELAY_MS = 1_000;
const MAX_HANDOVER_DELAY_MS = 30_000;

interface WindowLike {
  addEventListener?(type: string, fn: (event: { persisted?: boolean } | undefined) => void): void;
}

interface Intervals {
  setInterval(fn: () => void, ms: number): unknown;
  clearInterval(id: unknown): void;
}

export interface SharedFeedOptions extends Partial<Timers>, Partial<Intervals> {
  base: string;
  api: ApiClient;
  locks?: LockManager | null;
  BroadcastChannel?: typeof globalThis.BroadcastChannel | null;
  window?: WindowLike;
  now?: () => number;
  EventSource?: typeof globalThis.EventSource;
}

type Follower = Required<FeedHandlers>;

interface Entry {
  followers: Set<Follower>;
  status: FeedStatus;
  dropped: boolean;
}

interface Relay {
  tabs: Set<string>;
  status: FeedStatus;
  stop: (() => void) | null;
}

interface Holding {
  stream: Feed;
  relays: Map<string, Relay>;
  ready: boolean;
}

interface Message {
  type: string;
  from: string;
  path?: string;
  paths?: string[];
  data?: unknown;
  status?: FeedStatus;
  to?: string;
  delay?: number;
}

export function sharedFeed({
  base,
  api,
  locks = globalThis.navigator?.locks,
  BroadcastChannel = globalThis.BroadcastChannel,
  window = globalThis,
  now = () => Date.now(),
  setInterval = (fn, ms) => globalThis.setInterval(fn, ms),
  clearInterval = (id) => globalThis.clearInterval(id as number),
  EventSource = globalThis.EventSource,
  setTimeout = systemTimers.setTimeout,
  clearTimeout = systemTimers.clearTimeout,
}: SharedFeedOptions): Feed {
  const ownStream = () => streamFeed({ base, api, EventSource, setTimeout, clearTimeout });
  if (!locks || !BroadcastChannel) return ownStream();

  const me = Math.random().toString(36).slice(2) + Date.now().toString(36);
  const channel = new BroadcastChannel(NAME);
  const post = (msg: Omit<Message, "from">) => channel.postMessage({ ...msg, from: me });

  /** What this tab follows: path -> its followers and last status. */
  const local = new Map<string, Entry>();
  let waiting: AbortController | null = null; // this tab's queued lock request
  let stealing = false; // a steal is in flight
  let tabLock = false;
  let heartbeat: unknown = null;
  let holderTab: string | null = null; // the stream-holding tab, once heard from
  let pingedAt: number | null = null; // when this tab pinged the stream-holding tab without an answer yet

  // A follower that throws must not keep the others from hearing.
  function each(entry: Entry, fn: (follower: Follower) => void) {
    for (const follower of [...entry.followers]) {
      try {
        fn(follower);
      } catch {
        // its own problem; the feed goes on
      }
    }
  }
  function report(entry: Entry, status: FeedStatus) {
    entry.status = status;
    each(entry, (f) => f.onStatus(status));
  }

  /** A status for a path this tab follows, from the stream-holding tab. */
  function hear(path: string, status: FeedStatus) {
    const entry = local.get(path);
    if (!entry) return;
    switch (status) {
      case "paused":
        entry.dropped = true;
        if (entry.status !== "paused") report(entry, "paused");
        break;
      case "live":
        if (entry.dropped) report(entry, "resynced");
        entry.dropped = false;
        if (entry.status !== "live") report(entry, "live");
        break;
      case "connecting":
        if (!entry.dropped && entry.status !== "connecting") report(entry, "connecting");
        break;
      // resynced is derived here from paused then live: a new stream-holding
      // tab's stream reports a fresh connecting and live, not resynced.
    }
  }
  /**
   * The stream-holding tab changed or went away: what this tab follows is
   * paused until the new one follows it again, then reported resynced.
   */
  function handover() {
    for (const entry of local.values()) {
      if (entry.status === "connecting" && !entry.dropped) continue; // never live: nothing missed
      entry.dropped = true;
      if (entry.status !== "paused") report(entry, "paused");
    }
  }
  function deliver(path: string, data: unknown) {
    const entry = local.get(path);
    if (entry) each(entry, (f) => f.onMessage(data));
  }

  // ── the stream-holding tab ──────────────────────────────────────────────
  let holding: Holding | null = null;

  function relay(tab: string, path: string) {
    const h = holding!;
    let r = h.relays.get(path);
    if (r) {
      r.tabs.add(tab);
      if (r.stop) {
        if (tab === me) hear(path, r.status);
        else post({ type: "status", path, status: r.status, to: tab });
      }
      return;
    }
    r = { tabs: new Set([tab]), status: "connecting", stop: null };
    h.relays.set(path, r);
    if (h.ready) start(path, r);
  }
  /** Follows a relayed path on this tab's stream. */
  function start(path: string, r: Relay) {
    r.stop = holding!.stream.follow(path, {
      onMessage(data) {
        deliver(path, data);
        post({ type: "event", path, data });
      },
      onStatus(status) {
        if (status === "resynced") return; // live follows; tabs derive it
        r.status = status;
        hear(path, status);
        post({ type: "status", path, status });
      },
    });
  }
  function unrelay(tab: string, path: string) {
    const r = holding!.relays.get(path);
    if (!r || !r.tabs.delete(tab) || r.tabs.size > 0) return;
    holding!.relays.delete(path);
    r.stop?.();
  }
  /** Sets what a tab follows to paths, following and unfollowing the difference. */
  function relayAll(tab: string, paths: readonly string[]) {
    const want = new Set(paths);
    for (const [path, r] of [...holding!.relays]) if (r.tabs.has(tab) && !want.has(path)) unrelay(tab, path);
    for (const path of want) if (!holding!.relays.get(path)?.tabs.has(tab)) relay(tab, path);
  }
  function stopRelays() {
    for (const r of holding!.relays.values()) r.stop?.();
  }

  // Handovers that follow one another quickly (tabs crashing or taking over in
  // turn) back off like a dropped stream: the new stream-holding tab waits 1 s,
  // doubling to 30 s, before following again. A handover after a quiet spell is
  // at once.
  let handedOverAt: number | null = null; // when this tab last heard of a new stream-holding tab
  let handoverDelay = 0; // how long that one waited: it travels with the announcement
  function nextHandoverDelay() {
    const quick = handedOverAt !== null && now() - handedOverAt < MAX_HANDOVER_DELAY_MS;
    handoverDelay = quick ? Math.min(Math.max(handoverDelay * 2, FIRST_HANDOVER_DELAY_MS), MAX_HANDOVER_DELAY_MS) : 0;
    handedOverAt = now();
    return handoverDelay;
  }

  /** Opens this tab's stream and asks every tab what it follows. */
  function announce() {
    const h: Holding = { stream: ownStream(), relays: new Map(), ready: false };
    holding = h;
    holderTab = me;
    pingedAt = null;
    handover();
    const delay = nextHandoverDelay();
    post({ type: "holder", delay });
    relayAll(me, [...local.keys()]);
    const begin = () => {
      if (holding !== h) return; // deposed while waiting
      h.ready = true;
      for (const [path, r] of h.relays) start(path, r);
    };
    if (delay > 0) setTimeout(begin, delay);
    else begin();
  }

  function hold(): Promise<never> {
    stealing = false;
    waiting?.abort(); // a request queued while stealing would be granted out of turn
    waiting = null;
    startHeartbeat();
    announce();
    // Held until the tab goes away (the browser releases it then) or another tab takes it over.
    return new Promise<never>(() => {});
  }

  /** Pauses what this tab follows and tells the stream-holding tab about it. */
  function rejoin() {
    handover();
    post({ type: "paths", paths: [...local.keys()] });
  }

  /** Another tab holds the stream now: close this one and follow through it. */
  function stepDown() {
    if (!holding) return;
    stopRelays();
    holding = null;
    requestLock();
  }

  let holds = 0; // counts this tab's grants, so a late rejection is matched to its own
  function lockRequest(options: LockOptions): Promise<void> {
    let grant = 0;
    return locks!
      .request(NAME, options, () => {
        grant = ++holds;
        return hold();
      })
      .then(
        () => {},
        () => {
          // Taken over by another tab. A request this tab withdrew to steal was
          // never granted, and an older grant says nothing about the lock it holds now.
          if (grant !== 0 && grant === holds && holding) {
            stepDown();
            rejoin();
          }
        },
      );
  }

  function requestLock() {
    if (!tabLock) {
      // Held for as long as this tab lives: the stream-holding tab drops the
      // paths of a tab whose lock is gone, so a crashed tab's follows end.
      tabLock = true;
      locks!.request(`${NAME}/tab/${me}`, () => new Promise<never>(() => {})).catch(() => {});
    }
    startHeartbeat();
    if (holding || waiting || stealing) return;
    waiting = new AbortController();
    void lockRequest({ signal: waiting.signal });
  }

  function startHeartbeat() {
    if (!heartbeat) heartbeat = setInterval(check, PING_MS);
  }

  /**
   * The stream-holding tab drops the paths of tabs that are gone. Every other
   * tab pings it, and takes over from one that stays silent.
   */
  function check() {
    if (holding) {
      void sweep();
      return;
    }
    if (local.size === 0) {
      clearInterval(heartbeat);
      heartbeat = null;
      return;
    }
    if (pingedAt === null) {
      pingedAt = now();
      post({ type: "ping" });
      return;
    }
    if (now() - pingedAt < SILENCE_MS) return;
    pingedAt = null;
    waiting?.abort();
    waiting = null;
    stealing = true;
    void lockRequest({ steal: true }).then(() => {
      stealing = false;
    });
  }

  async function sweep() {
    const h = holding;
    let state: LockManagerSnapshot;
    try {
      state = await locks!.query();
    } catch {
      return;
    }
    if (!h || holding !== h) return;
    const alive = new Set([...(state.held ?? []), ...(state.pending ?? [])].map((l) => l.name));
    const tabs = new Set([...h.relays.values()].flatMap((r) => [...r.tabs]));
    for (const tab of tabs) if (tab !== me && !alive.has(`${NAME}/tab/${tab}`)) relayAll(tab, []);
  }

  channel.onmessage = ({ data: msg }: MessageEvent<Message | null>) => {
    if (!msg || msg.from === me) return;
    if (msg.type === "holder") {
      handedOverAt = now();
      handoverDelay = msg.delay ?? 0;
    }
    if (holding && msg.type === "holder") stepDown(); // it took the lock while this tab was frozen
    if (holding) {
      switch (msg.type) {
        case "ping":
          return post({ type: "pong" });
        case "follow":
          return relay(msg.from, msg.path!);
        case "unfollow":
          return unrelay(msg.from, msg.path!);
        case "paths":
          return relayAll(msg.from, msg.paths ?? []);
        case "bye":
          return relayAll(msg.from, []);
      }
      return;
    }
    if (["holder", "event", "status", "pong"].includes(msg.type)) {
      holderTab = msg.from;
      pingedAt = null;
    }
    switch (msg.type) {
      case "holder":
        return rejoin();
      case "bye":
        if (msg.from === holderTab) handover();
        return;
      case "event":
        return deliver(msg.path!, msg.data);
      case "status":
        if (!msg.to || msg.to === me) hear(msg.path!, msg.status!);
        return;
    }
  };

  window.addEventListener?.("pagehide", () => post({ type: "bye" }));
  // A page restored from the back/forward cache said bye on its way in, and
  // missed whatever happened since: follow everything again.
  window.addEventListener?.("pageshow", (event) => {
    if (!event?.persisted || local.size === 0) return;
    if (holding) {
      stopRelays();
      announce();
    } else {
      rejoin();
      requestLock();
    }
  });

  return {
    follow(path, { onMessage, onStatus = () => {} }) {
      const follower: Follower = { onMessage, onStatus };
      let entry = local.get(path);
      if (!entry) {
        entry = { followers: new Set([follower]), status: "connecting", dropped: false };
        local.set(path, entry);
        follower.onStatus("connecting");
        if (holding) relay(me, path);
        else post({ type: "follow", path });
        requestLock();
      } else {
        entry.followers.add(follower);
        follower.onStatus(entry.status);
      }

      const followed = entry;
      let following = true;
      return () => {
        if (!following) return;
        following = false;
        followed.followers.delete(follower);
        if (followed.followers.size > 0) return;
        local.delete(path);
        if (holding) unrelay(me, path);
        else post({ type: "unfollow", path });
      };
    },
  };
}

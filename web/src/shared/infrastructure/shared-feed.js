// The page event stream, shared by every harness tab of a browser profile
// (ADR 0011). Over plain HTTP/1.1 the browser allows six connections per host
// across all its tabs, so a stream per tab spent them all at five or six tabs
// and every other request waited.
//
// One tab, the stream-holding tab, holds a Web Lock and with it the only
// stream (a stream-feed.js). The browser releases the lock when that tab
// closes or crashes, and the next tab waiting for it takes over. Tabs talk
// over a BroadcastChannel: each tells the stream-holding tab what it follows,
// and it follows the union on the server, counting followers per path across
// tabs, and relays every event and status to the tabs.
//
// A hung stream-holding tab keeps its lock, so the others check that it still
// answers: each pings it, and one that hears nothing for SILENCE_MS takes the
// lock from it. The ping is answered from the message handler, so a busy or
// throttled background tab answers in time. A deposed tab that runs again
// closes its stream and follows through the new stream-holding tab.
//
// It keeps feed.js's contract, so gateways cannot tell it from stream-feed.js.
// A browser without Web Locks or BroadcastChannel gets a stream per tab, as
// before.

import { FeedStatus } from "../domain/feed.js";
import { streamFeed } from "./stream-feed.js";

const NAME = "harness-page-event-stream";
const PING_MS = 2_000;
const SILENCE_MS = 10_000;
const FIRST_HANDOVER_DELAY_MS = 1_000;
const MAX_HANDOVER_DELAY_MS = 30_000;

/**
 * @param {{
 *   base: string,
 *   api: import("./api.js").ApiClient,
 *   locks?: LockManager | null,
 *   BroadcastChannel?: typeof globalThis.BroadcastChannel | null,
 *   window?: { addEventListener: (type: string, fn: (event: any) => void) => void },
 *   now?: () => number,
 *   setInterval?: typeof globalThis.setInterval,
 *   clearInterval?: typeof globalThis.clearInterval,
 *   EventSource?: typeof globalThis.EventSource,
 *   setTimeout?: typeof globalThis.setTimeout,
 *   clearTimeout?: typeof globalThis.clearTimeout,
 * }} options
 * @returns {import("./feed.js").Feed}
 */
export function sharedFeed({
  base,
  api,
  locks = globalThis.navigator?.locks,
  BroadcastChannel = globalThis.BroadcastChannel,
  window = globalThis,
  now = () => Date.now(),
  setInterval = globalThis.setInterval.bind(globalThis),
  clearInterval = globalThis.clearInterval.bind(globalThis),
  EventSource = globalThis.EventSource,
  setTimeout = globalThis.setTimeout.bind(globalThis),
  clearTimeout = globalThis.clearTimeout.bind(globalThis),
}) {
  const ownStream = () => streamFeed({ base, api, EventSource, setTimeout, clearTimeout });
  if (!locks || !BroadcastChannel) return ownStream();

  const me = Math.random().toString(36).slice(2) + Date.now().toString(36);
  const channel = new BroadcastChannel(NAME);
  const post = (msg) => channel.postMessage({ ...msg, from: me });

  /** What this tab follows: path -> its followers and last status. */
  const local = new Map();
  let waiting = null; // the AbortController of this tab's queued lock request
  let stealing = false; // a steal is in flight
  let tabLock = false;
  let heartbeat = null;
  let holderTab = null; // the stream-holding tab, once heard from
  let pingedAt = null; // when this tab pinged the stream-holding tab without an answer yet

  // A follower that throws must not keep the others from hearing.
  function each(entry, fn) {
    for (const f of [...entry.followers]) {
      try {
        fn(f);
      } catch {
        // its own problem; the feed goes on
      }
    }
  }
  function report(entry, status) {
    entry.status = status;
    each(entry, (f) => f.onStatus(status));
  }

  /** A status for a path this tab follows, from the stream-holding tab. */
  function hear(path, status) {
    const entry = local.get(path);
    if (!entry) return;
    switch (status) {
      case FeedStatus.PAUSED:
        entry.dropped = true;
        if (entry.status !== FeedStatus.PAUSED) report(entry, FeedStatus.PAUSED);
        break;
      case FeedStatus.LIVE:
        if (entry.dropped) report(entry, FeedStatus.RESYNCED);
        entry.dropped = false;
        if (entry.status !== FeedStatus.LIVE) report(entry, FeedStatus.LIVE);
        break;
      case FeedStatus.CONNECTING:
        if (!entry.dropped && entry.status !== FeedStatus.CONNECTING) report(entry, FeedStatus.CONNECTING);
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
      if (entry.status === FeedStatus.CONNECTING && !entry.dropped) continue; // never live: nothing missed
      entry.dropped = true;
      if (entry.status !== FeedStatus.PAUSED) report(entry, FeedStatus.PAUSED);
    }
  }
  function deliver(path, data) {
    const entry = local.get(path);
    if (entry) each(entry, (f) => f.onMessage(data));
  }

  // ── the stream-holding tab ──────────────────────────────────────────────
  /**
   * @type {null | {
   *   stream: import("./feed.js").Feed,
   *   relays: Map<string, { tabs: Set<string>, status: string, stop: (() => void) | null }>,
   *   ready: boolean,
   * }}
   */
  let holding = null;

  function relay(tab, path) {
    let r = holding.relays.get(path);
    if (r) {
      r.tabs.add(tab);
      if (r.stop) {
        if (tab === me) hear(path, r.status);
        else post({ type: "status", path, status: r.status, to: tab });
      }
      return;
    }
    r = { tabs: new Set([tab]), status: FeedStatus.CONNECTING, stop: null };
    holding.relays.set(path, r);
    if (holding.ready) start(path, r);
  }
  /** Follows a relayed path on this tab's stream. */
  function start(path, r) {
    r.stop = holding.stream.follow(path, {
      onMessage(data) {
        deliver(path, data);
        post({ type: "event", path, data });
      },
      onStatus(status) {
        if (status === FeedStatus.RESYNCED) return; // live follows; tabs derive it
        r.status = status;
        hear(path, status);
        post({ type: "status", path, status });
      },
    });
  }
  function unrelay(tab, path) {
    const r = holding.relays.get(path);
    if (!r || !r.tabs.delete(tab) || r.tabs.size > 0) return;
    holding.relays.delete(path);
    r.stop?.();
  }
  /** Sets what a tab follows to paths, following and unfollowing the difference. */
  function relayAll(tab, paths) {
    const want = new Set(paths);
    for (const [path, r] of [...holding.relays]) if (r.tabs.has(tab) && !want.has(path)) unrelay(tab, path);
    for (const path of want) if (!holding.relays.get(path)?.tabs.has(tab)) relay(tab, path);
  }
  function stopRelays() {
    for (const r of holding.relays.values()) r.stop?.();
  }

  // Handovers that follow one another quickly (tabs crashing or taking over
  // in turn) back off like a dropped stream: the new stream-holding tab waits
  // 1 s, doubling to 30 s, before following again. A handover after a quiet
  // spell is at once.
  let handedOverAt = null; // when this tab last heard of a new stream-holding tab
  // and how long that one waited: it travels with the announcement.
  let handoverDelay = 0;
  function nextHandoverDelay() {
    const quick = handedOverAt !== null && now() - handedOverAt < MAX_HANDOVER_DELAY_MS;
    handoverDelay = quick ? Math.min(Math.max(handoverDelay * 2, FIRST_HANDOVER_DELAY_MS), MAX_HANDOVER_DELAY_MS) : 0;
    handedOverAt = now();
    return handoverDelay;
  }

  /** Opens this tab's stream and asks every tab what it follows. */
  function announce() {
    const h = { stream: ownStream(), relays: new Map(), ready: false };
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

  function hold() {
    stealing = false;
    waiting?.abort(); // a request queued while stealing would be granted out of turn
    waiting = null;
    startHeartbeat();
    announce();
    // Held until the tab goes away (the browser releases it then) or another
    // tab takes it over.
    return new Promise(() => {});
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
  function lockRequest(options) {
    let grant = 0;
    return locks
      .request(NAME, options, () => {
        grant = ++holds;
        return hold();
      })
      .catch(() => {
        // Taken over by another tab. A request this tab withdrew to steal was
        // never granted, and an older grant says nothing about the lock it
        // holds now.
        if (grant !== 0 && grant === holds && holding) {
          stepDown();
          rejoin();
        }
      });
  }

  function requestLock() {
    if (!tabLock) {
      // Held for as long as this tab lives: the stream-holding tab drops the
      // paths of a tab whose lock is gone, so a crashed tab's follows end.
      tabLock = true;
      locks.request(`${NAME}/tab/${me}`, () => new Promise(() => {})).catch(() => {});
    }
    startHeartbeat();
    if (holding || waiting || stealing) return;
    waiting = new AbortController();
    lockRequest({ signal: waiting.signal });
  }

  function startHeartbeat() {
    if (!heartbeat) heartbeat = setInterval(check, PING_MS);
  }

  /**
   * The stream-holding tab drops the paths of tabs that are gone. Every other
   * tab pings it, and takes over from one that stays silent.
   */
  function check() {
    if (holding) return sweep();
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
    lockRequest({ steal: true }).then(() => {
      stealing = false;
    });
  }

  async function sweep() {
    const h = holding;
    let state;
    try {
      state = await locks.query();
    } catch {
      return;
    }
    if (holding !== h) return;
    const alive = new Set([...state.held, ...state.pending].map((l) => l.name));
    const tabs = new Set([...h.relays.values()].flatMap((r) => [...r.tabs]));
    for (const tab of tabs) if (tab !== me && !alive.has(`${NAME}/tab/${tab}`)) relayAll(tab, []);
  }

  channel.onmessage = ({ data: msg }) => {
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
          return relay(msg.from, msg.path);
        case "unfollow":
          return unrelay(msg.from, msg.path);
        case "paths":
          return relayAll(msg.from, msg.paths);
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
        return deliver(msg.path, msg.data);
      case "status":
        if (!msg.to || msg.to === me) hear(msg.path, msg.status);
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
      const follower = { onMessage, onStatus };
      let entry = local.get(path);
      if (!entry) {
        entry = { followers: new Set([follower]), status: FeedStatus.CONNECTING, dropped: false };
        local.set(path, entry);
        follower.onStatus(FeedStatus.CONNECTING);
        if (holding) relay(me, path);
        else post({ type: "follow", path });
        requestLock();
      } else {
        entry.followers.add(follower);
        follower.onStatus(entry.status);
      }

      let following = true;
      return () => {
        if (!following) return;
        following = false;
        entry.followers.delete(follower);
        if (entry.followers.size > 0) return;
        local.delete(path);
        if (holding) unrelay(me, path);
        else post({ type: "unfollow", path });
      };
    },
  };
}

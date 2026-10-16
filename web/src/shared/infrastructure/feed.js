// Server-sent events with the same recovery as the TUI's live.Follower: a
// dropped stream reports `paused`, retries with backoff (1s doubling to 30s),
// and reports `resynced` on reconnecting, because changes were missed.
//
// A page holds one long-lived stream, /api/page_events (ADR 0011): the
// browser allows only a few connections per host across all its tabs. Its
// followers (the rail, the sessions page, the inbox, …) follow a kind of
// change on it with followPage; each gets only its kind's payload, unwrapped
// from the envelope. The stream opens on the tick after its first follower
// joins, so the followers a page registers as it starts are all known and the
// URL carries their project, and it closes with its last follower. One that
// joins late is told the stream's status at once.
//
// follow(path) is the older stream per path, kept for the streams that do not
// ride the page stream yet (a session's events).

import { FeedStatus } from "../domain/feed.js";

const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 30_000;

/**
 * What a follower of the page stream follows: a kind, with what that kind
 * needs (a project follower names its project).
 *
 * @typedef {{kind: "project", projectId: string} | {kind: "notifications"}
 *   | {kind: "usage", projectId: string} | {kind: "projects"}} PageTopic
 */

/**
 * The kinds of the page stream a follower can follow. A kind says how its
 * followers shape the stream's query (`query`, given the kind's followers in
 * the order they joined and the query the stream has now), which followers
 * hear an envelope (`hears`, given the query the stream was opened with), and
 * what of the envelope they get (`payload`). A kind that adds to the query
 * reconnects the stream when a follower changes what it asks for. An opt-in
 * kind (`optIn`) is carried only once a follower has asked for it in `with`.
 * Envelopes of kinds not listed here are ignored.
 *
 * @type {Record<string, {
 *   optIn?: boolean,
 *   query?: (followers: any[], current: Record<string, string>) => Record<string, string>,
 *   hears?: (follower: any, query: Record<string, string>) => boolean,
 *   payload: (envelope: any) => any,
 * }>}
 */
const KINDS = {
  // What /api/events sent: the stream follows the project its latest project
  // follower asked for, and keeps it when they have all left. A follower with
  // no project (a page outside one) does not move it.
  project: {
    query: (followers, current) => ({
      project_id: followers.findLast((f) => f.topic.projectId)?.topic.projectId ?? current.project_id ?? "",
    }),
    hears: (follower, query) => follower.topic.projectId === query.project_id,
    payload: (envelope) => envelope.change,
  },
  // What /api/notifications/events sent: the envelope without its kind.
  notifications: {
    payload: ({ kind, ...rest }) => rest,
  },
  // What /api/projects/:p/usage/events sent, under record: the usage of the
  // stream's project, which its latest usage follower with a project names.
  usage: {
    optIn: true,
    query: (followers) => {
      const projectId = followers.findLast((f) => f.topic.projectId)?.topic.projectId;
      return projectId ? { project_id: projectId } : {};
    },
    hears: (follower, query) => follower.topic.projectId === query.project_id,
    payload: (envelope) => envelope.record,
  },
  // What /api/project_events sent, under change.
  projects: {
    optIn: true,
    payload: (envelope) => envelope.change,
  },
};

/**
 * @typedef {object} Feed
 * @property {(path: string, handlers: {
 *   onMessage: (data: any) => void,
 *   onStatus?: (status: import("../domain/feed.js").FeedStatus) => void,
 * }) => () => void} follow  Follows the stream at path; returns the function that stops following it.
 * @property {(topic: PageTopic, handlers: {
 *   onMessage: (payload: any) => void,
 *   onStatus?: (status: import("../domain/feed.js").FeedStatus) => void,
 * }) => () => void} followPage  Follows one kind of the page stream; returns the function that stops following it.
 * @property {() => string} pageStreamId  The page stream's id from its hello; "" while it is not open.
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
  /**
   * One EventSource, reopened with backoff when it drops, and the followers
   * it reports its status to. `url()` is asked on every open; `route` hands a
   * parsed message to the followers; `onClose` hears the EventSource end.
   */
  function stream({ url, route, deferOpen, onClose }) {
    const followers = new Set();
    let source = null;
    let timer = null;
    let delay = FIRST_RETRY_MS;
    let opened = false;
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
      timer = null;
      source = new EventSource(base + url());
      source.onopen = () => {
        delay = FIRST_RETRY_MS;
        opened = true;
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
        route(data, each);
      };
      source.onerror = () => {
        // EventSource's own reconnect has no backoff; take over.
        source.close();
        source = null;
        onClose?.();
        if (followers.size === 0) return;
        dropped = true;
        report(FeedStatus.PAUSED);
        timer = setTimeout(open, delay);
        delay = Math.min(delay * 2, MAX_RETRY_MS);
      };
    }

    return {
      followers,
      add(follower) {
        followers.add(follower);
        if (followers.size === 1) {
          opened = false;
          dropped = false;
          delay = FIRST_RETRY_MS;
          report(FeedStatus.CONNECTING);
          if (deferOpen) timer = setTimeout(open, 0);
          else open();
        } else {
          follower.onStatus(status);
        }
      },
      /** @returns {boolean} the stream has no followers left and is closed */
      remove(follower) {
        followers.delete(follower);
        if (followers.size > 0) return false;
        clearTimeout(timer);
        timer = null;
        source?.close();
        source = null;
        onClose?.();
        return true;
      },
      /**
       * Opens the stream again at once, for a URL that changed. Changes may
       * have been missed in between, so a stream that was live resyncs. One
       * that has not opened yet, or is waiting to retry, opens with the new
       * URL anyway.
       */
      reconnect() {
        if (!source) return;
        source.close();
        onClose?.();
        if (opened) dropped = true;
        open();
      },
    };
  }

  /** @type {Map<string, ReturnType<typeof stream>>} */
  const streams = new Map();

  // The page stream: the query it is open with, and the one its followers ask
  // for. Its hello names it (stream_id) until it ends, for the calls that act
  // on the stream, such as following a session.
  let query = {};
  let streamId = "";
  function wanted() {
    let next = {};
    for (const [kind, def] of Object.entries(KINDS)) {
      if (!def.query) continue;
      const followers = [...page.followers].filter((f) => f.topic.kind === kind);
      next = { ...next, ...def.query(followers, query) };
    }
    // The opt-in kinds a follower asked for, and those the stream carries
    // already: like the project, a kind stays when its followers have left.
    const carried = (query.with ?? "").split(",");
    const opted = Object.keys(KINDS).filter(
      (kind) => KINDS[kind].optIn && (carried.includes(kind) || [...page.followers].some((f) => f.topic.kind === kind)),
    );
    if (opted.length) next.with = opted.join(",");
    return next;
  }
  const page = stream({
    deferOpen: true,
    onClose: () => (streamId = ""),
    url() {
      query = wanted();
      return `/page_events?${new URLSearchParams(query)}`;
    },
    route(envelope, each) {
      if (envelope?.kind === "hello") streamId = String(envelope.stream_id ?? "");
      const def = envelope && Object.hasOwn(KINDS, envelope.kind) ? KINDS[envelope.kind] : null;
      if (!def) return; // the hello, and kinds this client does not know
      const payload = def.payload(envelope);
      each((f) => {
        if (f.topic.kind === envelope.kind && (!def.hears || def.hears(f, query))) f.onMessage(payload);
      });
    },
  });

  return {
    follow(path, { onMessage, onStatus = () => {} }) {
      let s = streams.get(path);
      if (!s) {
        s = stream({ url: () => path, route: (data, each) => each((f) => f.onMessage(data)) });
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

    followPage(topic, { onMessage, onStatus = () => {} }) {
      if (!Object.hasOwn(KINDS, topic?.kind)) throw new Error(`no such kind on the page stream: ${topic?.kind}`);
      const follower = { topic, onMessage, onStatus };
      page.add(follower);
      if (new URLSearchParams(wanted()).toString() !== new URLSearchParams(query).toString()) page.reconnect();

      let following = true;
      return () => {
        if (!following) return;
        following = false;
        page.remove(follower);
      };
    },

    pageStreamId: () => streamId,
  };
}

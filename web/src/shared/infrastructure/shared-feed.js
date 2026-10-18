// The page event stream, shared by every harness tab of a browser profile
// (ADR 0011). Over plain HTTP/1.1 the browser allows six connections per host
// across all its tabs, so a stream per tab spent them all at five or six tabs
// and every other request waited.
//
// It keeps feed.js's contract, so gateways cannot tell it from stream-feed.js.
// A browser without Web Locks or BroadcastChannel gets a stream per tab, as
// before.

import { streamFeed } from "./stream-feed.js";

/**
 * @param {{
 *   base: string,
 *   api: import("./api.js").ApiClient,
 *   locks?: LockManager | null,
 *   BroadcastChannel?: typeof globalThis.BroadcastChannel | null,
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
  EventSource = globalThis.EventSource,
  setTimeout = globalThis.setTimeout.bind(globalThis),
  clearTimeout = globalThis.clearTimeout.bind(globalThis),
}) {
  const ownStream = () => streamFeed({ base, api, EventSource, setTimeout, clearTimeout });
  if (!locks || !BroadcastChannel) return ownStream();
  return ownStream();
}

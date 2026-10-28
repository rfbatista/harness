// When an open Agents page re-reads the catalog (contract section 6): on the
// one-key `agent_catalog_changed` event of the page feed, when the follower is
// ended and resumes (the feed reports `resynced`), when the window regains
// focus (edits by hand), and when the person asks (Reload from disk). Pure:
// the timers and the read are given.
//
// Triggers that arrive close together are one read: a save publishes one
// event, but an import or a script publishes many. A trigger that arrives while
// a read is in flight asks for one more read after it, so the page never
// settles on what the earlier read saw.

import { FeedStatus } from "../../../shared/domain/feed.js";

/** How long triggers are gathered before the read starts. */
export const REFRESH_DELAY_MS = 150;

/**
 * The paths an `agent_catalog_changed` event names, or null when the message
 * is anything else on the page feed (project, git_credential, git_identity …).
 * An event without paths (a project's MCP server list changed) is still one.
 * @param {unknown} data
 * @returns {string[] | null}
 */
export function catalogEventPaths(data) {
  if (data === null || typeof data !== "object" || Array.isArray(data)) return null;
  if (!("agent_catalog_changed" in data)) return null;
  const body = /** @type {any} */ (data).agent_catalog_changed;
  const paths = body && Array.isArray(body.paths) ? body.paths : [];
  return paths.filter((p) => typeof p === "string");
}

/**
 * @typedef {object} RefreshRequest
 * @property {string[]} paths  what the events named, without repeats
 * @property {string[]} reasons  "event", "resynced", "focus", "manual"
 *
 * @param {{
 *   reload: (request: RefreshRequest) => Promise<void> | void,
 *   timers?: { setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void },
 *   delay?: number,
 * }} deps
 */
export function refreshScheduler({ reload, timers = globalThis, delay = REFRESH_DELAY_MS }) {
  let timer = null;
  let running = false;
  let disposed = false;
  let pending = null;
  let paths = [];
  let reasons = [];
  let dropped = false;

  const take = () => {
    const request = { paths, reasons };
    paths = [];
    reasons = [];
    return request;
  };
  const note = (reason, named = []) => {
    if (disposed) return;
    if (!reasons.includes(reason)) reasons.push(reason);
    for (const p of named) if (!paths.includes(p)) paths.push(p);
  };
  async function run() {
    timer = null;
    running = true;
    try {
      await reload(take());
    } catch {
      // The page says why a read failed; the scheduler goes on.
    } finally {
      running = false;
    }
    if (reasons.length > 0 && !disposed) start();
  }
  function start() {
    if (timer !== null || running) return;
    timer = timers.setTimeout(run, delay);
  }
  const trigger = (reason, named) => {
    if (disposed) return;
    note(reason, named);
    start();
  };

  return {
    /** A message of the page feed: only the catalog's own event reads. */
    event(data) {
      const named = catalogEventPaths(data);
      if (named !== null) trigger("event", named);
    },
    /** The feed's status: a drop followed by a resume (or resync) missed events. */
    status(status) {
      if (status === FeedStatus.PAUSED) dropped = true;
      else if (status === FeedStatus.RESYNCED || (status === FeedStatus.LIVE && dropped)) {
        dropped = false;
        trigger("resynced");
      }
    },
    /** The window regained focus. */
    focus() {
      trigger("focus");
    },
    /** Reload from disk: now, without waiting for the burst to settle. */
    now() {
      note("manual");
      if (timer !== null) timers.clearTimeout(timer);
      timer = null;
      if (running) return;
      timer = timers.setTimeout(run, 0);
    },
    /** The page went away: nothing is read after this. */
    dispose() {
      disposed = true;
      if (timer !== null) timers.clearTimeout(timer);
      timer = null;
    },
  };
}

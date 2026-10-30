// Whether the code on screen follows the worktree (UX §5, ADR 0048): a
// running session's view is live; a code_changed makes the list follow by
// itself, and the hunks on screen stale when they are older than the list
// (never moving them under the reader); a session that is not running sends
// no events, so its view is stopped: read on open and on Reload; a removed
// worktree is gone. A fixed ref has no state.

/** @typedef {"live"|"stale"|"stopped"|"gone"|""} LiveState */

import { plural } from "./code.js";

export const LIVE = "live";
export const STALE = "stale";
export const STOPPED = "stopped";
export const GONE = "gone";
export const FIXED = "";

/**
 * The source bar's state word; a stale one names how many files changed
 * since the hunks were loaded, when that is known.
 * @param {LiveState} state
 * @param {{ count?: number }} [about]
 */
export function stateWord(state, { count = 0 } = {}) {
  if (state === STALE && count > 0) return `${plural(count, "file")} changed`;
  return { live: "live", stale: "Changed on disk", stopped: "stopped", gone: "worktree gone" }[state] ?? "";
}

/**
 * The view's next state when the code changed on disk.
 * @param {LiveState} state
 * @param {{ untouched: boolean }} reader  untouched: at the very top and not scrolled since it loaded
 * @returns {{ state: LiveState, apply: boolean, announce: boolean }}
 *   apply: re-read what is on screen now; announce: say "Code changed on disk" (once per stale episode)
 */
export function onChange(state, { untouched }) {
  if (state === GONE || state === FIXED || state === STOPPED) return { state, apply: false, announce: false };
  if (state === LIVE && untouched) return { state: LIVE, apply: true, announce: false };
  return { state: STALE, apply: false, announce: state !== STALE };
}

/** After Reload: live again (unless the worktree is gone or the session is not running). */
export const onReload = (state) => (state === STALE ? LIVE : state);

/** The session stopped: nothing follows the worktree any more, unless it is gone. */
export const onEnded = (state) => (state === GONE ? GONE : STOPPED);

/** Whether Reload (R) has anything to re-read: a view that follows a worktree, running or not. */
export const canReload = (state) => state === LIVE || state === STALE || state === STOPPED;

export const ANNOUNCE_CHANGED = "Code changed on disk";

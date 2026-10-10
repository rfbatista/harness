// Whether the code on screen follows the worktree (UX §5): a running
// session's view is live; a code_changed for what is being read makes it
// stale, never moving it under the reader, unless the reader is at the very
// top and has not scrolled; a removed worktree is gone. A fixed ref, or an
// ended session, has no state.

/** @typedef {"live"|"stale"|"gone"|""} LiveState */

export const LIVE = "live";
export const STALE = "stale";
export const GONE = "gone";
export const FIXED = "";

/** The source bar's state word. */
export function stateWord(state) {
  return { live: "live", stale: "Changed on disk", gone: "worktree gone" }[state] ?? "";
}

/**
 * The view's next state when the code changed on disk.
 * @param {LiveState} state
 * @param {{ untouched: boolean }} reader  untouched: at the very top and not scrolled since it loaded
 * @returns {{ state: LiveState, apply: boolean, announce: boolean }}
 *   apply: re-read what is on screen now; announce: say "Code changed on disk" (once per stale episode)
 */
export function onChange(state, { untouched }) {
  if (state === GONE || state === FIXED) return { state, apply: false, announce: false };
  if (state === LIVE && untouched) return { state: LIVE, apply: true, announce: false };
  return { state: STALE, apply: false, announce: state !== STALE };
}

/** After Reload: live again (unless the worktree is gone or the session ended). */
export const onReload = (state) => (state === STALE ? LIVE : state);

/** The session ended: the code is fixed, unless it is gone. */
export const onEnded = (state) => (state === GONE ? GONE : FIXED);

export const ANNOUNCE_CHANGED = "Code changed on disk";

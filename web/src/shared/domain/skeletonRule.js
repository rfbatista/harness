// When a skeleton shows (Glossary: Skeleton). A load that ends within about
// 120 ms shows none, so a fast load stays calm. Once shown, a skeleton stays
// at least about 300 ms, so it does not blink. An error removes it at once, so
// a list that fails shows its own error and never a skeleton that stays.
// The timers are given, so a test runs on a virtual clock.
//
//   const rule = skeletonRule({ onChange: ({ visible, fade }) => … });
//   rule.begin();   // a load starts
//   rule.end();     // it finished: the content replaces the skeleton (fade: true if one was shown)
//   rule.fail();    // it failed: the skeleton goes now, the list's error shows
//   rule.stop();    // the component goes away: no timer is left

export const SKELETON_SHOW_AFTER_MS = 120;
export const SKELETON_MIN_SHOWN_MS = 300;

/** @typedef {{ setTimeout: (fn: () => void, ms: number) => any, clearTimeout: (id: any) => void, now: () => number }} Timers */

/** @type {Timers} */
const browserTimers = {
  setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
  clearTimeout: (id) => globalThis.clearTimeout(id),
  now: () => globalThis.performance?.now?.() ?? Date.now(),
};

/**
 * @param {{
 *   onChange: (state: { visible: boolean, fade: boolean }) => void,
 *   timers?: Timers,
 *   showAfter?: number,
 *   minShown?: number,
 * }} options  fade is true when the skeleton goes because the content arrived: the content fades in where it was
 */
export function skeletonRule({ onChange, timers = browserTimers, showAfter = SKELETON_SHOW_AFTER_MS, minShown = SKELETON_MIN_SHOWN_MS }) {
  let visible = false;
  let running = false;
  let timer = null;
  let shownAt = 0;

  const clear = () => {
    if (timer !== null) timers.clearTimeout(timer);
    timer = null;
  };
  const hide = (fade) => {
    clear();
    running = false;
    if (!visible) return;
    visible = false;
    onChange({ visible: false, fade });
  };

  return {
    get visible() {
      return visible;
    },

    /** A load is under way: begun, not yet ended or failed. */
    get running() {
      return running;
    },

    begin() {
      if (running) return;
      running = true;
      if (visible) {
        clear(); // still up from the load before: it carries on
        return;
      }
      timer = timers.setTimeout(() => {
        timer = null;
        visible = true;
        shownAt = timers.now();
        onChange({ visible: true, fade: false });
      }, showAfter);
    },

    end() {
      if (!running) return;
      if (!visible) {
        clear();
        running = false;
        return;
      }
      const wait = shownAt + minShown - timers.now();
      if (wait <= 0) return hide(true);
      clear();
      running = false;
      timer = timers.setTimeout(() => hide(true), wait);
    },

    fail() {
      hide(false);
    },

    stop() {
      hide(false);
    },
  };
}

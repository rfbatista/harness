// When a skeleton shows (Glossary: Skeleton). A load that ends within about
// 120 ms shows none, so a fast load stays calm. Once shown, a skeleton stays
// at least about 300 ms, so it does not blink. An error removes it at once, so
// a list that fails shows its own error and never a skeleton that stays.
// The timers are given, so a test runs on a virtual clock.
//
//   const rule = skeletonRule({ onChange: ({ visible, fade }) => … });
//   rule.begin();   // a load starts
//   await rule.end(); // it finished: resolves once the skeleton is gone (at once if none showed); show the content then, so nothing stacks under it (fade: true if one was shown)
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
  let waiting = [];

  const clear = () => {
    if (timer !== null) timers.clearTimeout(timer);
    timer = null;
  };
  const hide = (fade) => {
    clear();
    running = false;
    const wasVisible = visible;
    visible = false;
    if (wasVisible) onChange({ visible: false, fade });
    for (const resolve of waiting.splice(0)) resolve();
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
        clear(); // still up from the load before: it carries on, and that load's content is let in
        for (const resolve of waiting.splice(0)) resolve();
        return;
      }
      timer = timers.setTimeout(() => {
        timer = null;
        visible = true;
        shownAt = timers.now();
        onChange({ visible: true, fade: false });
      }, showAfter);
    },

    /** @returns {Promise<void>} resolves when the skeleton is gone: at once if none shows, else after its minimum */
    end() {
      if (!running) return Promise.resolve();
      if (!visible) {
        clear();
        running = false;
        return Promise.resolve();
      }
      const wait = shownAt + minShown - timers.now();
      if (wait <= 0) {
        hide(true);
        return Promise.resolve();
      }
      clear();
      running = false;
      timer = timers.setTimeout(() => hide(true), wait);
      return new Promise((resolve) => waiting.push(resolve));
    },

    fail() {
      hide(false);
    },

    stop() {
      hide(false);
    },
  };
}

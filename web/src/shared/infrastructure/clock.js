// Time as a dependency, so "2m ago" is testable.

/** @typedef {{ now: () => Date }} Clock */

/** @type {Clock} */
export const systemClock = Object.freeze({ now: () => new Date() });

/** A clock stopped at `at`; tests move it with `set`. */
export function fixedClock(at) {
  let current = new Date(at);
  return {
    now: () => new Date(current),
    set(next) {
      current = new Date(next);
    },
  };
}

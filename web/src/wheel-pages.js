// Wheel and touch scrolling turned into page keys, for a full-screen program
// that asked for the mouse (claude draws on the alternate screen and scrolls
// its own view on PageUp/PageDown). The terminal socket carries keys, not
// mouse reports, so a scroll becomes PageUp or PageDown once it has moved
// far enough: one key per gesture step, never a burst.

/** WheelEvent.deltaMode values. */
const DOM_DELTA_LINE = 1;
const DOM_DELTA_PAGE = 2;

/**
 * @param {{ step: () => number, line: () => number }} size
 *   step: how far, in px, a scroll moves before it is one page key (half the pane);
 *   line: a row's height, in px, for wheels that count lines
 */
export function wheelPages({ step, line }) {
  let travelled = 0;
  return {
    /**
     * Adds a scroll of delta (positive: down, towards newer) and says which
     * key it makes, if any. A change of direction starts over.
     * @param {number} delta
     * @param {number} [mode] WheelEvent.deltaMode
     * @returns {"PageUp" | "PageDown" | null}
     */
    add(delta, mode = 0) {
      const px = mode === DOM_DELTA_LINE ? delta * line() : mode === DOM_DELTA_PAGE ? delta * step() * 2 : delta;
      if (px === 0) return null;
      if (Math.sign(px) !== Math.sign(travelled)) travelled = 0;
      travelled += px;
      const threshold = Math.max(step(), 1);
      if (Math.abs(travelled) < threshold) return null;
      // One key however far it went: a fling is a page, not a dozen.
      const key = travelled < 0 ? "PageUp" : "PageDown";
      travelled = 0;
      return key;
    },
    reset() {
      travelled = 0;
    },
  };
}

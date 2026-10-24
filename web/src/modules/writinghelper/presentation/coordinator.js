// One panel open at a time, and one answer to "can the helper run": a pen
// that learns the helper is unavailable tells every pen on the page.

/** @typedef {{ reason: string, fixUrl: string }} Unavailable */

export function createCoordinator() {
  /** @type {Set<any>} */
  const pens = new Set();
  let open = null;
  /** @type {Unavailable | null} */
  let unavailable = null;

  return {
    add(pen) {
      pens.add(pen);
      if (unavailable) pen.setUnavailable(unavailable);
    },
    remove(pen) {
      pens.delete(pen);
      if (open === pen) open = null;
    },
    /** The pen opens its panel: the one open before it closes, discarding its draft. */
    claim(pen) {
      if (open && open !== pen) open.close(false);
      open = pen;
    },
    release(pen) {
      if (open === pen) open = null;
    },
    /** @param {Unavailable} info */
    markUnavailable(info) {
      unavailable = info;
      for (const pen of pens) pen.setUnavailable(info);
    },
  };
}

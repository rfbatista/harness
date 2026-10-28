// Which file sections are near the viewport: the lazy sections draw their
// lines when they come within about two viewports of the scroll position.
// An intersection observer does it off the scroll handler; the unit tests
// pass their own.

/**
 * @typedef {object} Proximity
 * @property {(el: Element) => void} watch
 * @property {(el: Element) => void} unwatch
 * @property {() => void} disconnect
 */

/** How far outside the scroller counts as near: two viewports above and below. */
const MARGIN = "200% 0px 200% 0px";

/**
 * @param {Element|null} root  what scrolls (null: the page)
 * @param {(changes: [Element, boolean][]) => void} onChange
 * @returns {Proximity}
 */
export function intersectionProximity(root, onChange) {
  const observer = new IntersectionObserver((entries) => onChange(entries.map((e) => [e.target, e.isIntersecting])), { root, rootMargin: MARGIN });
  return {
    watch: (el) => observer.observe(el),
    unwatch: (el) => observer.unobserve(el),
    disconnect: () => observer.disconnect(),
  };
}

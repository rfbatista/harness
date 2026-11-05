// Prefetch on hover (ADR 0064): when the pointer rests on a link for about
// 100 ms, or the link gets keyboard focus, the browser asks for the page that
// a click on it would ask for, so that the click finds it ready.
//
// The prefetched answer is held by the browser's own cache for 5 s: the server
// marks it private, max-age=5 only when it is asked ahead (X-Harness-Prefetch),
// and varies on that header, so only a click that sends it finds the answer.
// This module sends it for the one click that follows a prefetch (use), and
// for no other, and none once anything has changed: a write this page makes
// (`api-write`, from the API client) or a change the live feed announces
// (`feed-change`, from the rail) ends every hold, so a click after an action
// never shows a page read before it (ADR 0062). A link is prefetched once per hover. A link to the page that
// is open, and any link that is not a plain GET page link, is never prefetched.
//
// htmx's own prefetching (its preload extension) works on the elements that
// htmx processes; the links that Alpine renders are not among them, so the
// hover is caught on the document, as the click is.

export const HOVER_MS = 100;
export const HOLD_MS = 5000;
export const PREFETCH_HEADER = "X-Harness-Prefetch";

/**
 * @param {{
 *   doc?: Document,
 *   isPageLink: (link: HTMLAnchorElement, here: Location) => boolean,
 *   fetch?: typeof fetch,
 *   setTimeout?: typeof setTimeout,
 *   clearTimeout?: typeof clearTimeout,
 *   now?: () => number,
 * }} deps
 */
export function startPrefetch({ doc = document, isPageLink, fetch = doc.defaultView.fetch.bind(doc.defaultView), setTimeout = globalThis.setTimeout, clearTimeout = globalThis.clearTimeout, now = Date.now }) {
  /** The pages asked for ahead, by URL, with when each stops being held. */
  const held = new Map();
  /** The links the pointer rests on, waiting for their 100 ms. */
  const resting = new Map();
  /** The links already prefetched during this hover. */
  const done = new WeakSet();

  const win = doc.defaultView;
  const endHolds = () => held.clear();
  win.addEventListener("api-write", endHolds);
  win.addEventListener("feed-change", endHolds);

  const linkOf = (target) => (target instanceof Element ? target.closest("a[href]") : null);
  const withoutHash = (url) => url.split("#")[0];
  const prefetchable = (link) =>
    !!link.closest("#app-shell") &&
    isPageLink(link, doc.location) &&
    !link.hasAttribute("data-method") &&
    withoutHash(link.href) !== withoutHash(doc.location.href);

  function prefetch(link) {
    const url = link.href;
    if ((held.get(url) ?? 0) > now()) return;
    held.set(url, now() + HOLD_MS);
    setTimeout(() => held.delete(url), HOLD_MS);
    // The same request that the click makes, plus the mark that lets the
    // server say the answer may be held.
    fetch(url, {
      credentials: "same-origin",
      headers: { "HX-Request": "true", "HX-Boosted": "true", "HX-Target": "app-shell", "HX-Current-URL": doc.location.href, [PREFETCH_HEADER]: "1" },
    })
      .then((response) => response.text())
      .catch(() => held.delete(url));
  }

  doc.addEventListener("mouseover", (e) => {
    const link = linkOf(e.target);
    if (!link || resting.has(link) || done.has(link) || !prefetchable(link)) return;
    resting.set(
      link,
      setTimeout(() => {
        resting.delete(link);
        done.add(link);
        prefetch(link);
      }, HOVER_MS),
    );
  });

  doc.addEventListener("mouseout", (e) => {
    const link = linkOf(e.target);
    if (!link || link.contains(/** @type {Node | null} */ (e.relatedTarget))) return;
    clearTimeout(resting.get(link));
    resting.delete(link);
    done.delete(link);
  });

  doc.addEventListener("focusin", (e) => {
    const link = linkOf(e.target);
    // Keyboard focus only: a click focuses the link too, and asks for the page itself.
    if (link && link.matches(":focus-visible") && prefetchable(link)) prefetch(link);
  });

  return {
    /**
     * Whether the click on url finds a page that was asked for ahead and is
     * still held. It is used once: the next click asks again.
     * @param {string} url
     */
    use(url) {
      const until = held.get(url);
      held.delete(url);
      return until !== undefined && until > now();
    },
  };
}

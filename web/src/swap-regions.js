// Swap regions (ADR 0005): htmx swaps a server-owned part of a page in place
// when a link marked for it is followed, and the page's own URL answers with
// just that region. Configured here once, for every region, and wired only
// from the composition root.
//
// A region is [data-swap-region] with an id; its links carry hx-boost,
// hx-target and hx-swap (shell.SwapLink). While a swap is in flight the region
// is aria-busy; after it, the region is at its top, its first heading has
// focus, and any [data-swap-keep-scroll="key"] inside it keeps its scroll.
// A swap that fails loads the URL as a page.

/** @param {any} htmx @param {{ navigate: (url: string) => void, doc?: Document }} deps */
export function startSwapRegions(htmx, { navigate, doc = document }) {
  Object.assign(htmx.config, {
    allowEval: false,
    allowScriptTags: false,
    includeIndicatorStyles: false,
    // No snapshots: a restored snapshot would bring back dead Alpine markup.
    // Back and Forward load the page instead.
    historyCacheSize: 0,
    refreshOnHistoryMiss: true,
    scrollIntoViewOnBoost: false,
    selfRequestsOnly: true,
  });

  const regionOf = (el) => (el instanceof Element ? el.closest("[data-swap-region]") : null);
  let kept = new Map();

  doc.addEventListener("htmx:beforeRequest", (e) => regionOf(e.detail.target)?.setAttribute("aria-busy", "true"));
  doc.addEventListener("htmx:afterRequest", (e) => regionOf(e.detail.target)?.removeAttribute("aria-busy"));

  doc.addEventListener("htmx:beforeSwap", (e) => {
    const region = regionOf(e.detail.target);
    kept = new Map();
    for (const el of region?.querySelectorAll("[data-swap-keep-scroll]") ?? []) {
      kept.set(el.getAttribute("data-swap-keep-scroll"), el.scrollTop);
    }
  });

  doc.addEventListener("htmx:afterSettle", (e) => {
    const region = e.target instanceof Element && e.target.matches("[data-swap-region]") ? e.target : null;
    if (!region) return;
    region.scrollTop = 0;
    for (const el of region.querySelectorAll("[data-swap-keep-scroll]")) {
      el.scrollTop = kept.get(el.getAttribute("data-swap-keep-scroll")) ?? 0;
    }
    const heading = region.querySelector("h1, h2, h3");
    if (heading) {
      if (!heading.hasAttribute("tabindex")) heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
    }
  });

  const fallBack = (e) => navigate(e.detail.pathInfo.requestPath);
  doc.addEventListener("htmx:responseError", fallBack);
  doc.addEventListener("htmx:sendError", fallBack);
}

// Swap regions (ADR 0005): htmx swaps a server-owned part of a page in place
// when a link marked for it is followed, and the page's own URL answers with
// just that region. Configured here once, for every region, and wired only
// from the composition root.
//
// A region is [data-swap-region] with an id; its links carry hx-boost,
// hx-target and hx-swap (shell.SwapLink). While a swap is in flight the region
// is aria-busy (a link that swaps something else, like "Show more", is busy
// itself); after it, the region is at its top, its first heading has
// focus, any [data-swap-keep-scroll="key"] inside it keeps its scroll, and of
// the links that swap it, the one to the new address is aria-current (the
// list's highlight follows, also when the list is outside the region). A link
// to its own anchor (#c-<hash> on a history row) does not scroll the list.
// A swap that fails loads the URL as a page.
//
// A region may carry its own JSON seed (templ.JSONScript), so htmx keeps
// script elements; a fragment holding a script that could run is never
// inserted: the URL loads as a page instead.

/** @param {any} htmx @param {{ navigate: (url: string) => void, doc?: Document }} deps */
export function startSwapRegions(htmx, { navigate, doc = document }) {
  Object.assign(htmx.config, {
    allowEval: false,
    // JSON seeds travel with their region; executableScript keeps any
    // other script out.
    allowScriptTags: true,
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

  // A swap into a region shows the region busy; one that only adds to a
  // list ("Show more") shows its link busy.
  const busyOf = (e) => regionOf(e.detail.target) ?? e.detail.elt;
  doc.addEventListener("htmx:beforeRequest", (e) => busyOf(e)?.setAttribute("aria-busy", "true"));
  doc.addEventListener("htmx:afterRequest", (e) => busyOf(e)?.removeAttribute("aria-busy"));

  doc.addEventListener("htmx:beforeSwap", (e) => {
    if (e.detail.shouldSwap && executableScript(e.detail.serverResponse)) {
      e.preventDefault();
      navigate(e.detail.pathInfo.requestPath);
      return;
    }
    // The clicked link is where its anchor points: it is in view already.
    if (e.detail.pathInfo.anchor && e.detail.requestConfig?.elt?.id === e.detail.pathInfo.anchor) {
      e.detail.pathInfo.anchor = "";
    }
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
    for (const link of doc.querySelectorAll(`a[hx-target="#${region.id}"]`)) {
      if (link.closest("[data-swap-region]") === region) continue;
      if (new URL(link.href).pathname === doc.location.pathname) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
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

/**
 * Whether html holds a script other than a JSON one. It is parsed inertly
 * (DOMParser runs nothing), before anything reaches the page.
 * @param {string} html
 */
function executableScript(html) {
  const parsed = new DOMParser().parseFromString(html, "text/html");
  return [...parsed.querySelectorAll("script")].some(
    (script) => script.getAttribute("type")?.trim().toLowerCase() !== "application/json",
  );
}

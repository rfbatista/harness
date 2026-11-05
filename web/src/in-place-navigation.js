// In-place navigation (ADR 0064): a click on a link between pages swaps only
// what changes, without loading a new document. The shell carries the one
// target, swap and push-url (shell.AppShell), and one delegated click handler
// here asks htmx for the page: the links that Alpine renders (the rail's task
// rows, the lists) are not htmx's to process, so the click is caught on the
// document and every link inside the shell is a navigation alike. The server
// answers the page fragment, which htmx places in #main (the answer's
// HX-Retarget), or the app shell of another frame, which replaces the shell;
// the out-of-band items bring the title, the frame state and the stream bar.
// The rail, the top bar, the bell, the writing helper, the stores and the one
// event stream stay in the document through a fragment.
//
// A navigation is a request that names the app shell as its target. Anything
// the server answers without the X-Harness-Fragment mark (a redirect to a
// page that has none, an error) is never swapped: the URL loads as a page, so
// a full document never lands inside #main.
//
// History: the address bar follows each navigation (a push after the swap),
// and Back and Forward ask the server for the page of the entry they land on,
// with no snapshot of swapped pages. Every entry carries the id of the page
// that was on screen when it was written (the history methods are wrapped to
// add it, so a page's own pushState and replaceState keep it): an entry of
// the page on screen is the page's own routing (the code browser, the design
// system tabs, the agents catalog) and is left to it; an entry of another page
// is a navigation, so the page is swapped for it.
//
// Prefetch on hover is prefetch.js: the click uses what hover asked for.
//
// The swapped-out <main> is removed from the document, which makes Alpine
// call destroy() on its components; the new one boots from the seed in the
// fragment. After the swap, the new page's heading has focus and the page's
// title is announced, as it is for a swap region.

import { PREFETCH_HEADER, startPrefetch } from "./prefetch.js";

const NAV_TARGET = "app-shell";
const FRAGMENT_HEADER = "X-Harness-Fragment";

/**
 * @param {any} htmx
 * @param {{ Alpine: any, navigate: (url: string) => void, doc?: Document }} deps
 */
export function startInPlaceNavigation(htmx, { Alpine, navigate, doc = document }) {
  const win = doc.defaultView;
  const history = win.history;
  const prefetch = startPrefetch({ doc, isPageLink });
  const isNavigation = (detail) => detail.requestConfig?.headers?.["HX-Target"] === NAV_TARGET;

  // The page on screen: one id per document life, so that entries written
  // before a reload are never taken for the page that is on screen now.
  const life = Math.random().toString(36).slice(2);
  let pages = 0;
  let pageId = `${life}:${pages}`;
  // Where the frame on screen was last loaded from, for HX-Current-URL when
  // the address bar has moved ahead of it (Back and Forward).
  let shownUrl = doc.location.href;
  let restoring = false;

  for (const method of ["pushState", "replaceState"]) {
    const original = history[method].bind(history);
    history[method] = (state, title, url) => original(state === null || (typeof state === "object" && !Array.isArray(state)) ? { ...state, page: pageId } : state, title, url);
  }
  history.replaceState(history.state, "");

  win.addEventListener("popstate", (e) => {
    const entry = e.state?.page;
    if (entry === undefined || entry === pageId) return; // not an entry of ours, or the page's own
    e.stopImmediatePropagation();
    restoring = true;
    htmx.ajax("GET", doc.location.href, { target: `#${NAV_TARGET}`, swap: "outerHTML", headers: { "HX-Boosted": "true", "HX-Current-URL": shownUrl } });
  });

  doc.addEventListener("click", (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const link = e.target instanceof Element ? e.target.closest("a[href]") : null;
    if (!link || !link.closest(`#${NAV_TARGET}`) || !isPageLink(link, doc.location)) return;
    e.preventDefault();
    restoring = false; // a failed Back leaves no mark behind
    // A page asked for on hover is used by this click, once, from the browser's hold.
    const headers = { "HX-Boosted": "true", ...(prefetch.use(link.href) ? { [PREFETCH_HEADER]: "1" } : {}) };
    htmx.ajax("GET", link.href, { source: link, target: `#${NAV_TARGET}`, swap: "outerHTML", headers });
  });

  doc.addEventListener("htmx:beforeSwap", (e) => {
    if (!isNavigation(e.detail) || !e.detail.shouldSwap) return;
    if (!e.detail.xhr.getResponseHeader(FRAGMENT_HEADER)) {
      e.preventDefault();
      restoring = false;
      navigate(e.detail.pathInfo.requestPath);
    }
  });

  doc.addEventListener("htmx:oobAfterSwap", (e) => {
    if (e.target instanceof Element && e.target.id === "frame-state") applyFrameState(e.target);
  });

  doc.addEventListener("htmx:afterSettle", (e) => {
    if (!isNavigation(e.detail)) return;
    pageId = `${life}:${++pages}`;
    if (!restoring) {
      // A click on the link to the page that is open replaces its entry.
      const same = new URL(e.detail.pathInfo.responsePath, doc.location.href).href === doc.location.href;
      history[same ? "replaceState" : "pushState"](null, "", e.detail.pathInfo.responsePath);
    }
    restoring = false;
    shownUrl = doc.location.href;
    // htmx wrote the new title, which has no unread count: the bell adds it.
    Alpine.store("notifications")?.retitle?.();
    // The phone's drawer closes on a click, as the page load it replaced did.
    doc.defaultView?.dispatchEvent(new CustomEvent("rail-close"));
    const main = doc.getElementById("main");
    if (!main) return;
    doc.defaultView?.scrollTo(0, 0);
    main.scrollTop = 0;
    const heading = main.querySelector("h1, h2, h3");
    if (heading) {
      if (!heading.hasAttribute("tabindex")) heading.setAttribute("tabindex", "-1");
      heading.focus({ preventScroll: true });
    }
    doc.defaultView?.dispatchEvent(new CustomEvent("rail-announce", { detail: heading?.textContent?.trim() || doc.title }));
  });

  /**
   * The rail shows the page and the task that are open: the project page's
   * link is current, and the live rail marks the task. The page's rail reports
   * the connection to the stream bar only on the pages that follow its feed.
   * @param {HTMLElement} state
   */
  function applyFrameState(state) {
    const rail = doc.getElementById("rail");
    if (!rail) return;
    const page = state.dataset.page ?? "";
    const task = state.dataset.task ?? "";
    for (const item of rail.querySelectorAll("li[data-page]")) {
      const link = item.querySelector("a");
      if (item.dataset.page === page) link?.setAttribute("aria-current", "page");
      else link?.removeAttribute("aria-current");
    }
    const top = state.dataset.top ?? "";
    for (const link of doc.querySelectorAll("header.topbar a[href]")) {
      if (link.getAttribute("href") === top) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
    rail.dataset.currentTask = task;
    if (state.dataset.reportsFeed !== undefined) rail.setAttribute("data-reports-feed", "");
    else rail.removeAttribute("data-reports-feed");
    const data = Alpine.$data(rail);
    if (data && "currentTaskId" in data) {
      data.currentTaskId = task;
      data.reportsFeed = state.dataset.reportsFeed !== undefined;
    }
  }
}

/**
 * Whether a click on link is a navigation: a link to another page of this
 * server, not one that a swap region, a download, an Alpine handler or the
 * browser (a new tab, an anchor on this page) already owns.
 * @param {HTMLAnchorElement} link
 * @param {Location} here
 */
function isPageLink(link, here) {
  if (link.origin !== here.origin || (link.target && link.target !== "_self")) return false;
  if (link.hasAttribute("download") || link.hasAttribute("hx-boost") || link.hasAttribute("hx-target")) return false;
  const href = link.getAttribute("href") ?? "";
  if (href.startsWith("#") || href.startsWith("/api/") || href.startsWith("/static/")) return false;
  if (link.hash && link.pathname + link.search === here.pathname + here.search) return false;
  return ![...link.attributes].some((a) => a.name === "x-on:click" || a.name.startsWith("x-on:click.") || a.name.startsWith("@click"));
}

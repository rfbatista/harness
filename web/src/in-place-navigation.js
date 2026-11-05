// In-place navigation (ADR 0064): a click on a link between pages swaps only
// what changes, without loading a new document. The shell carries the one
// hx-boost (shell.AppShell), so every link inside it is a navigation; the
// server answers the page fragment, which htmx places in #main (the answer's
// HX-Retarget), and the out-of-band items bring the title, the frame state and
// the stream bar. The rail, the top bar, the bell, the writing helper, the
// stores and the one event stream stay in the document.
//
// A navigation is a request that names the app shell as its target. Anything
// the server answers without the X-Harness-Fragment mark (a page that has no
// fragment, a redirect to a full page, an error) is never swapped: the URL
// loads as a page, so a full document never lands inside #main.
//
// The swapped-out <main> is removed from the document, which makes Alpine
// call destroy() on its components; the new one boots from the seed in the
// fragment. After the swap, the new page's heading has focus and the page's
// title is announced, as it is for a swap region.

const NAV_TARGET = "app-shell";
const FRAGMENT_HEADER = "X-Harness-Fragment";

/**
 * @param {any} htmx
 * @param {{ Alpine: any, navigate: (url: string) => void, doc?: Document }} deps
 */
export function startInPlaceNavigation(htmx, { Alpine, navigate, doc = document }) {
  const isNavigation = (detail) => detail.requestConfig?.headers?.["HX-Target"] === NAV_TARGET;

  // The shell boosts every link and form inside it. A form is an action or
  // a filter that its page owns, and a link that downloads, that its page
  // handles itself or that leaves the pages stays a plain link.
  doc.addEventListener("htmx:beforeProcessNode", (e) => {
    const el = e.target;
    if (!(el instanceof Element) || el.hasAttribute("hx-boost")) return;
    if (el.tagName === "FORM" || (el.tagName === "A" && keepsItsOwnClick(el))) {
      el.setAttribute("hx-boost", "false");
    }
  });

  doc.addEventListener("htmx:beforeSwap", (e) => {
    if (!isNavigation(e.detail) || !e.detail.shouldSwap) return;
    if (!e.detail.xhr.getResponseHeader(FRAGMENT_HEADER)) {
      e.preventDefault();
      navigate(e.detail.pathInfo.requestPath);
    }
  });

  doc.addEventListener("htmx:oobAfterSwap", (e) => {
    if (e.target instanceof Element && e.target.id === "frame-state") applyFrameState(e.target);
  });

  doc.addEventListener("htmx:afterSettle", (e) => {
    if (!isNavigation(e.detail)) return;
    // htmx wrote the new title, which has no unread count: the bell adds it.
    Alpine.store("notifications")?.retitle?.();
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
 * A link that something else owns: a download, an API or asset URL, or one
 * that its Alpine component handles on click.
 * @param {Element} link
 */
function keepsItsOwnClick(link) {
  if (link.hasAttribute("download")) return true;
  const href = link.getAttribute("href") ?? "";
  if (href.startsWith("/api/") || href.startsWith("/static/")) return true;
  return [...link.attributes].some((a) => a.name === "x-on:click" || a.name.startsWith("x-on:click.") || a.name.startsWith("@click"));
}

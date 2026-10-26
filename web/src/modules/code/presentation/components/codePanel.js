// The session page's Code tab: a session's worktree as it is on disk, what it
// changed, and the map. Mounted by the sessions page for the selected session
// and kept behind the other tabs, so it counts the changed files for the
// tab's badge and follows code_changed while the session runs.
//
//   <template x-for="panel in codePanels" x-bind:key="panel.key">
//     <section x-data="codePanel(panel)" x-on:code-tab.window="tabShown" …>…</section>
//   </template>
//
// The sessions page talks to it through window events only (modules never
// import each other): `code-tab` {sessionId, shown} when the tab comes to the
// front or leaves it; it answers with `code-files-counted` {sessionId, count}.
// The panel owns the address bar while its tab is in front.

import { FIXED, LIVE } from "../../domain/live.js";
import { codeBrowser } from "./codeBrowser.js";
import { compose } from "./compose.js";

/** The stream bar's keys while the Code tab is in front. */
export const CODE_HINTS = Object.freeze([
  { keys: ["T"], label: "find file" },
  { keys: ["L"], label: "line" },
  { keys: ["]", "["], label: "files" },
  { keys: ["G", "C/F/M"], label: "views" },
  { keys: ["⇧E"], label: "expand" },
]);

/**
 * @typedef {object} PanelInput  what the sessions page passes
 * @property {string} key
 * @property {string} projectId
 * @property {string} taskId
 * @property {string} sessionId
 * @property {string} workspaceId
 * @property {string} repositoryId
 * @property {string} repositoryName
 * @property {string} branch
 * @property {string} baseRef
 * @property {boolean} live      the session is running
 * @property {boolean} shown     the Code tab is in front now
 * @property {boolean} expanded  the page's list is hidden for the Code tab (⇧E)
 * @property {string} seedId     the first paint's seed, when the server painted this panel
 */

/** @param {import("./codeBrowser.js").BrowserDeps} deps */
export const codePanel = (deps) => (/** @type {PanelInput} */ panel) => {
  let observer = null;
  const browser = codeBrowser(deps);
  const codeBrowserPopState = browser.popState;
  return compose(browser, {
    kind: "session",
    sessionId: "",
    shown: false,
    /** The window is wide enough (64rem) for the split view to show the list beside the detail. */
    wide: true,

    /** Expand only helps where the session list shows beside the code. */
    get canExpand() {
      return this.wide || this.expanded;
    },

    init() {
      this.sessionId = panel.sessionId;
      this.shown = !!panel.shown;
      this.expanded = !!panel.expanded;
      const seedEl = panel.seedId ? this.$el.ownerDocument.getElementById(panel.seedId) : null;
      let seeded = false;
      if (seedEl) {
        try {
          const seed = deps.gateway.decodeSeed(JSON.parse(seedEl.textContent ?? "null"));
          if (seed.route.sessionId === panel.sessionId) {
            this.startFrom(seed);
            seeded = true;
          }
        } catch {
          // a seed we cannot read: start as if none was painted
        }
      }
      if (!seeded) {
        this.startFrom(
          deps.gateway.decodeSeed({
            project_id: panel.projectId,
            route: { kind: "session", project_id: panel.projectId, task_id: panel.taskId, session_id: panel.sessionId },
            source: { workspace_id: panel.workspaceId },
            repository_name: panel.repositoryName,
            branch: panel.branch,
            base_ref: panel.baseRef,
          }),
        );
      }
      this.source = { ...this.source, workspaceId: panel.workspaceId, repositoryId: "" };
      this.repositoryName ||= panel.repositoryName;
      this.branch ||= panel.branch;
      this.baseRef ||= panel.baseRef;
      this.liveState = this.gone ? this.liveState : panel.live ? LIVE : FIXED;
      if (panel.live && !this.gone) this.follow();
      this.watchWidth();
      // The diff counts the badge whatever the view; read it once the page is idle.
      const first = () => {
        if (!this.diff) this.showDiff();
        this.readWorking();
        if (this.shown) this.enterView();
      };
      (deps.idle ?? ((cb) => setTimeout(cb, 0)))(first);
      if (this.shown) this.tabShown({ detail: { sessionId: this.sessionId, shown: true } });
    },

    destroy() {
      observer?.disconnect();
      this.stop();
      if (this.shown) this.$dispatch("stream-hints", null);
    },

    urlOwned() {
      return this.shown;
    },

    countFiles() {
      if (this.diff) this.$dispatch("code-files-counted", { sessionId: this.sessionId, count: this.diff.files.length });
    },
    countHeld(diff) {
      this.$dispatch("code-files-counted", { sessionId: this.sessionId, count: diff.files.length });
    },

    /** The sessions page brought the Code tab to the front, or left it. */
    tabShown(event) {
      const { sessionId, shown } = event.detail ?? {};
      if (sessionId !== this.sessionId) return;
      this.shown = shown;
      // After a replaced panel's destroy has cleared them.
      this.$nextTick(() => this.$dispatch("stream-hints", shown ? CODE_HINTS : null));
      if (!shown) return;
      (deps.history ?? globalThis.history)?.replaceState?.(null, "", this.currentHref());
      this.enterView();
    },

    currentHref() {
      return this.viewHref(this.view);
    },

    /** The page's `session-ended` {id}: this session's code is fixed now. */
    sessionEnded(event) {
      if (event.detail?.id === this.sessionId) this.ended();
    },

    /** Back and forward move the code view only while its tab is in front. */
    popState() {
      if (this.shown) codeBrowserPopState.call(this);
    },

    watchWidth() {
      const wide = globalThis.matchMedia?.("(width >= 64rem)");
      if (wide) {
        this.wide = wide.matches;
        wide.addEventListener?.("change", (e) => (this.wide = e.matches));
      }
      // The panel itself is as wide as its layout, and is there from the start
      // (the layout is drawn after init and may be redrawn). A hidden tab is
      // 0 wide: that says nothing about the window.
      const el = this.$el;
      if (!el || typeof ResizeObserver !== "function") return;
      const rem = parseFloat(getComputedStyle(el.ownerDocument.documentElement).fontSize) || 16;
      observer = new ResizeObserver((entries) => {
        const width = entries[0].contentRect.width;
        if (width > 0) this.measure(width, rem);
      });
      observer.observe(el);
    },
  });
};

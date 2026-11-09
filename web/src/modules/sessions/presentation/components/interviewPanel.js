// The interview panel beside a session's terminal: the glue between the page's
// DOM and the store "sessionsInterviews", which holds the interviews and the
// rules for when the panel is shown. It decides docked or sheet from the width
// of the session column, follows the session's interview feed while it is
// mounted, and claims Alt+Shift+Q for the page. It never moves the focus or the
// scroll of anything the person is using: an interview that arrives only draws.
//
//   <div class="[ agent-stage ]" x-data="sessionsInterviewPanel(sid)" x-on:keydown.window.capture="claimKey">
//     <div class="[ terminal ]" …>…</div>
//     <template x-if="dockShown"><aside class="[ interview ]">…</aside></template>
//     <template x-if="peeking">…the peek strip…</template>
//     <template x-if="sheetShown">…the sheet…</template>
//   </div>

import { dockedAt } from "../../domain/interviews.js";
import { bodyHtml } from "../interviewView.js";

/**
 * @param {{
 *   interviews: ReturnType<typeof import("../interviewStore.js").interviewStore>,
 *   observeResize?: (el: HTMLElement, onResize: () => void) => () => void,
 *   remPx?: () => number,  what a rem is in px, for the dock width
 *   doc?: Document,
 * }} deps
 */
export const interviewPanel = ({ interviews, observeResize = resizeObserver, remPx = rootFontSize, doc = globalThis.document }) => (sessionId = "") => {
  /** The stage. A method called from a control that has since left the page (the peek strip's Answer) cannot find it through $root, so it is kept. */
  let root = null;
  let stopObserving = () => {};
  let unfollow = () => {};
  /** Which control opened the sheet: where the focus goes back when it closes. */
  let openedBy = "toggle";
  /** The markdown last drawn into each body, so a store change does not redraw (and unselect) what has not changed. */
  const drawn = new WeakMap();

  return {
    sessionId,

    init() {
      root = this.$el;
      this.measure();
      stopObserving = observeResize(root, () => this.measure());
      unfollow = interviews.follow(sessionId, (text) => this.$dispatch("announce", { text }));
    },

    destroy() {
      stopObserving();
      unfollow();
      interviews.sheet = false;
    },

    /** Docks the panel when the session column is wide enough. */
    measure() {
      interviews.setDocked(dockedAt(root.clientWidth, remPx()));
    },

    // ── what the markup binds ────────────────────────────────────────────
    get view() {
      return interviews.view(sessionId);
    },
    get dockShown() {
      return interviews.docked && interviews.shown(sessionId);
    },
    get sheetShown() {
      return !interviews.docked && interviews.shown(sessionId);
    },
    get peeking() {
      return interviews.peeking(sessionId);
    },

    /** The markdown of a question's body, drawn as HTML into its element (x-effect). */
    placeBody(el, question) {
      if (drawn.get(el) === question.body) return;
      drawn.set(el, question.body);
      el.innerHTML = bodyHtml(question.body);
    },

    // ── what the person does ─────────────────────────────────────────────
    /** The peek strip's Answer: the sheet opens and takes the focus. */
    openSheet() {
      openedBy = "peek";
      interviews.reveal(sessionId);
      this.$nextTick(() => this.focusPanel());
    },

    /** The close button: hides the docked panel (a choice this browser keeps), or closes the sheet. */
    closePanel() {
      const sheet = this.sheetShown;
      interviews.dismissPanel(sessionId);
      if (sheet) this.$nextTick(() => this.focusOpener());
    },

    /** The sheet's scrim or Esc. */
    closeSheet() {
      interviews.dismissPanel(sessionId);
      this.$nextTick(() => this.focusOpener());
    },

    /**
     * Alt+Shift+Q: the page's, never the agent's. It opens the panel when it is
     * collapsed and focuses its first question's first control; pressed again
     * from inside the panel it gives the focus back to the terminal.
     * @param {KeyboardEvent} event  heard in the capture phase, before xterm
     */
    claimKey(event) {
      if (!event.altKey || !event.shiftKey || event.ctrlKey || event.metaKey || event.code !== "KeyQ") return;
      event.preventDefault();
      event.stopPropagation();
      if (event.repeat) return;
      const panel = root.querySelector("[data-interview-panel]");
      if (panel?.contains(doc.activeElement)) {
        // Under the sheet the terminal is inert: it takes the focus once the sheet is gone.
        if (this.sheetShown) interviews.dismissPanel(sessionId);
        this.$nextTick(() => this.$dispatch("terminal-focus-requested"));
        return;
      }
      if (!interviews.view(sessionId).open) return;
      openedBy = "toggle";
      interviews.reveal(sessionId);
      this.$nextTick(() => this.focusFirstControl());
    },

    /** The tab row's Questions toggle opened the sheet (the page says so): it takes the focus. */
    sheetOpened() {
      openedBy = "toggle";
      this.focusPanel();
    },

    /** The sheet took the focus: its scroller, which holds the questions. */
    focusPanel() {
      root.querySelector("[data-interview-scroll]")?.focus({ preventScroll: true });
    },

    focusFirstControl() {
      const first = root.querySelector("[data-interview-panel] .interview-question :is(input, textarea)");
      (first ?? root.querySelector("[data-interview-scroll]"))?.focus();
    },

    /** The sheet closed: the focus returns to the toggle or the peek strip that opened it. */
    focusOpener() {
      const toggle = doc.querySelector("[data-interview-toggle]");
      const peek = root.querySelector("[data-interview-peek] button");
      (openedBy === "peek" ? (peek ?? toggle) : (toggle ?? peek))?.focus();
    },
  };
};

const rootFontSize = () => parseFloat(globalThis.getComputedStyle?.(globalThis.document.documentElement).fontSize) || 16;

function resizeObserver(el, onResize) {
  if (typeof ResizeObserver === "undefined") return () => {};
  const observer = new ResizeObserver(() => onResize());
  observer.observe(el);
  return () => observer.disconnect();
}

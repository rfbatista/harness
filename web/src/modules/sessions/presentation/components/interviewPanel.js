// The interview panel beside a session's terminal: the glue between the page's
// DOM and the store "sessionsInterviews", which holds the interviews and the
// rules for when the panel is shown. It decides docked or sheet from the width
// of the session column, follows the session's interview feed while it is
// mounted, and claims Alt+Shift+Q for the page. It never moves the focus or the
// scroll of anything the person is using: an interview that arrives only draws.
// The person's answers are the store's; this turns the controls' events into
// them (a change, a number key, Enter on the recommendation) and moves the
// focus between questions the way the UX study's keyboard table says.
//
//   <div class="[ agent-stage ]" x-data="sessionsInterviewPanel(sid)" x-on:keydown.window.capture="claimKey">
//     <div class="[ terminal ]" …>…</div>
//     <template x-if="dockShown"><aside class="[ interview ]">…</aside></template>
//     <template x-if="peeking">…the peek strip…</template>
//     <template x-if="sheetShown">…the sheet…</template>
//   </div>

import { dockedAt } from "../../domain/interviews.js";
import { bodyHtml, footerView, optionForKey } from "../interviewView.js";

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

    // ── the answers the markup binds ─────────────────────────────────────
    checked: (card, q, value) => interviews.isChecked(card.id, q.at, value),
    ownOpen: (card, q) => interviews.ownOpen(card.id, q.at),
    answered: (card, q) => interviews.answered(card.id, q.at),
    textOf: (card, q) => interviews.textOf(card.id, q.at),
    noteOf: (card, q) => interviews.noteOf(card.id, q.at),
    progress: (card) => footerView(interviews.progress(card.id)),

    /** A note opens by itself once it holds text; the person's own opening and closing is left to the disclosure. */
    openNote(el, card, q) {
      if (interviews.noteOf(card.id, q.at).trim() !== "" && !el.open) el.open = true;
    },

    // ── what the person answers ──────────────────────────────────────────
    /** An option or Write my own was ticked (radio: selected). Write my own takes the focus into its box. */
    choose(card, q, event) {
      const input = event.target;
      interviews.choose(card.id, q.at, input.value, input.checked);
      if (input.value === "own" && input.checked) this.$nextTick(() => input.closest("fieldset")?.querySelector("[data-interview-own]")?.focus());
    },

    type(card, q, event) {
      interviews.type(card.id, q.at, event.target.value);
    },

    typeNote(card, q, event) {
      interviews.note(card.id, q.at, event.target.value);
    },

    use(card, q) {
      interviews.use(card.id, q.at);
    },

    /** Accept all recommended: fills the blank questions, sends nothing. */
    acceptAll(card) {
      interviews.acceptAll(card.id);
    },

    /**
     * Keys on an option row: 1–8 select that row in the drawn order, Enter on
     * the recommendation selects it and moves on to the next question. Every
     * other key (arrows, Space, Tab) is the browser's.
     * @param {KeyboardEvent} event
     */
    optionKey(event, card, q) {
      const input = event.target;
      if (input.tagName !== "INPUT" || event.altKey || event.ctrlKey || event.metaKey) return;
      if (event.key === "Enter" && input.closest("label")?.hasAttribute("data-recommended")) {
        event.preventDefault();
        interviews.choose(card.id, q.at, input.value, true);
        this.focusNext(input.closest("fieldset"));
        return;
      }
      const option = event.shiftKey ? null : optionForKey(q, event.key);
      if (!option) return;
      event.preventDefault();
      interviews.choose(card.id, q.at, option.value, true);
      input.closest(".options").querySelector(`input[value="${option.value}"]`)?.focus();
    },

    /** Esc in a text box: back to the question's option group. It is the panel's, so it neither closes the sheet nor hides the panel. */
    leaveBox(event) {
      event.preventDefault();
      event.stopPropagation();
      const box = event.target;
      const fieldset = box.closest("fieldset");
      const other = [...fieldset.querySelectorAll("textarea")].find((t) => t !== box);
      (this.optionGroup(fieldset) ?? (box.hasAttribute("data-interview-note") ? other : fieldset.querySelector("summary")))?.focus();
    },

    /** The checked option of a question, or its first: where Tab lands in the group. */
    optionGroup(fieldset) {
      return fieldset.querySelector(".options input:checked") ?? fieldset.querySelector(".options input");
    },

    /** A question's first control: its option group, or its box when it is a text question. */
    focusQuestion(fieldset) {
      (this.optionGroup(fieldset) ?? fieldset.querySelector("textarea"))?.focus();
    },

    /** The question after this one, or the Send button when it was the last. */
    focusNext(fieldset) {
      const next = fieldset.nextElementSibling;
      if (next?.matches("fieldset")) this.focusQuestion(next);
      else fieldset.closest("section")?.querySelector("[data-interview-send]")?.focus();
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

    /** The first unanswered question of the newest interview (its first question when all are answered). */
    focusFirstControl() {
      const card = this.view.cards[0];
      const fieldsets = root.querySelectorAll("[data-interview-panel] section")[0]?.querySelectorAll("fieldset");
      const at = Math.max(0, card ? card.questions.findIndex((q) => !interviews.answered(card.id, q.at)) : 0);
      const target = fieldsets?.[at];
      if (target) this.focusQuestion(target);
      else root.querySelector("[data-interview-scroll]")?.focus();
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

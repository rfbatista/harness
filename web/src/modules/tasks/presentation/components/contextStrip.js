// The task page's context strip: which band's panel is open, if any. At most
// one is, so the strip's height stays bounded. The bands (feature, reviews,
// token savers) sit inside it and read `expanded` and call `toggle`.
//
//   <div x-data="tasksContextStrip" data-open="reviews" data-stored="" role="group" aria-label="Task context"
//        x-on:keydown="key" x-on:reviews-read="reviewsRead"> …bands…
//
// data-open is the panel the server painted open (the person's task_band
// cookie, or Reviews while a request waits); data-stored is the cookie's own
// value. A press or Escape remembers the choice; a link to a band (#reviews,
// #review-{id}, #feature) opens it for the visit and remembers nothing.
// Escape closes the open panel and puts focus back on its button; the arrow
// keys, Home and End move between the buttons without opening them.

import { bandOfHash, buttonToFocus } from "../contextStripView.js";

/** Escape in a field belongs to the field. */
const FIELDS = "input, textarea, select";

/** @param {{ prefs: import("../../domain/ports.js").BandPrefs, hash?: () => string }} deps */
export const contextStrip = ({ prefs, hash = () => globalThis.location?.hash ?? "" }) => () => {
  /** The component's root: inside a handler $el is the element that fired. */
  let root = null;

  return {
    /** The open band's key ("feature"); "" when none is. */
    open: "",
    /** The cookie's value: what the person chose last. */
    stored: "",
    /** The person has pressed a button on this page: their press wins over what the page learns later. */
    touched: false,

    init() {
      root = this.$el;
      this.open = this.$el.dataset.open ?? "";
      this.stored = this.$el.dataset.stored ?? "";
      this.open = bandOfHash(hash()) || this.open;
    },

    expanded(key) {
      return this.open === key;
    },

    /** A summary button was pressed: open its panel, or close it when it is the open one. */
    toggle(key) {
      this.open = this.open === key ? "" : key;
      this.touched = true;
      prefs.setOpen(this.open || "none");
    },

    /** A key pressed anywhere in the strip. */
    key(event) {
      if (event.key === "Escape") return this.escape(event);
      const buttons = [...root.querySelectorAll(".cs-btn")].filter((b) => !b.hidden);
      const from = buttons.indexOf(event.target);
      if (from < 0) return;
      const to = buttonToFocus(buttons.length, from, event.key);
      if (to < 0) return;
      event.preventDefault();
      buttons[to].focus();
    },

    /** Escape: close the open panel and return to its button. */
    escape(event) {
      if (this.open === "" || event.target.matches?.(FIELDS)) return;
      const button = root.querySelector(`[data-band="${this.open}"]`);
      this.toggle(this.open);
      button?.focus();
    },

    /**
     * The task's requests were read. A stored Reviews opens when any exist,
     * settled ones included: the server paints it open only for waiting ones.
     * @param {{ detail: { has: boolean } }} event
     */
    reviewsRead(event) {
      if (this.stored === "reviews" && !this.touched && this.open === "" && event.detail.has) this.open = "reviews";
    },
  };
};

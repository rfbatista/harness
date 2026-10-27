// The anchored dialog: a .popup with role="dialog" or "alertdialog" in the top
// layer, opened from a trigger it hands the focus back to. Used where a
// control needs more than a menu (a form, a confirm, a sheet of actions) but
// must not push the page around: nothing it shows takes room in the flow.
//
//   <button type="button" id="session-more" …>⋯</button>
//   <div class="[ popup ]" role="alertdialog" id="session-delete" data-trap
//        x-data="popupDialog" x-on:dialog-open.window="openFrom($event)">
//     … <button type="button" data-initial-focus x-on:click="close()">Keep it</button>
//       <button type="button" x-on:click="act('delete')">Delete session</button>
//   </div>
//
// Opened by `dialog-open` on window, with { id, anchor }. The focus goes to
// [data-initial-focus], else the first control. Esc closes it and returns the
// focus to the anchor, and goes no further, so a terminal behind it never sees
// it. It tells the page it closed (`dialog-close` on window, { id }), however
// it closed, and closes on `dialog-dismiss` ({ id }) from the page. A dialog also closes on a press outside it; an alertdialog only by its
// own buttons or Esc. data-trap keeps Tab inside. On a phone it is a sheet.

import { defaultEnv, floating } from "./popup.js";

const FOCUSABLE = 'button:not([disabled]), [href], select:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export class AnchoredDialog {
  /**
   * @param {HTMLElement} el  the .popup
   * @param {import("./popup.js").Env} env
   * @param {() => void} [onClose]  runs once each time it closes
   */
  constructor(el, env = defaultEnv(), onClose = () => {}) {
    this.el = el;
    this.env = env;
    this.onClose = onClose;
    this.anchor = null;
    this.floating = null;
    el.setAttribute("popover", "manual");
    el.addEventListener("keydown", (e) => this.key(e));
  }

  get isOpen() {
    return this.floating?.isOpen ?? false;
  }

  get isAlert() {
    return this.el.getAttribute("role") === "alertdialog";
  }

  /** @param {HTMLElement} anchor  the control that opened it, and gets the focus back */
  open(anchor) {
    if (this.isOpen) this.close({ refocus: false });
    this.anchor = anchor;
    this.floating = floating(this.el, {
      anchor,
      env: this.env,
      align: () => "end",
      onDismiss: () => {
        if (!this.isAlert) this.close({ refocus: false });
      },
    });
    anchor.setAttribute("aria-expanded", "true");
    this.floating.open({ sheet: this.floating.isPhone() });
    const first = this.el.querySelector("[data-initial-focus]") ?? this.controls()[0];
    first?.focus();
  }

  /** @param {{ refocus?: boolean }} [options] */
  close({ refocus = true } = {}) {
    if (!this.floating) return;
    const wasOpen = this.floating.isOpen;
    this.floating.close();
    this.floating = null;
    this.anchor?.setAttribute("aria-expanded", "false");
    if (wasOpen && refocus) this.anchor?.focus();
    if (wasOpen) this.onClose();
  }

  controls() {
    return [...this.el.querySelectorAll(FOCUSABLE)].filter((c) => !c.closest("[hidden]"));
  }

  key(e) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      this.close();
      return;
    }
    if (e.key !== "Tab" || !this.el.hasAttribute("data-trap")) return;
    const controls = this.controls();
    if (controls.length === 0) return;
    const first = controls[0];
    const last = controls[controls.length - 1];
    const at = this.el.ownerDocument.activeElement;
    if (e.shiftKey && at === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  }
}

/** Alpine.data("popupDialog", popupDialog()). */
export const popupDialog = (env) => () => ({
  // Declared, so init's assignment lands here: an undeclared one would land on
  // the outermost scope (the page), shared by every dialog on it.
  _dialog: null,
  init() {
    this._dialog = new AnchoredDialog(this.$el, env ?? defaultEnv(), () => this.$dispatch("dialog-close", { id: this.$el.id }));
  },
  get isOpen() {
    return this._dialog?.isOpen ?? false;
  },
  /** @param {{ detail?: { id: string, anchor: HTMLElement } }} event */
  // this.$el is the element of the expression that called the method (a
  // button inside, for act), so the dialog's own element names it.
  openFrom(event) {
    if (event.detail?.id === this._dialog.el.id && event.detail.anchor) this._dialog.open(event.detail.anchor);
  },
  close() {
    this._dialog.close();
  },
  /** The page asked it closed (dialog-dismiss, { id }). */
  dismissFrom(event) {
    if (event.detail?.id === this._dialog.el.id) this._dialog.close();
  },
  /** A button inside it was pressed: tells the page, which owns what it does (dialog-action, { id, action }). */
  act(action) {
    this.$dispatch("dialog-action", { id: this._dialog.el.id, action });
  },
  destroy() {
    this._dialog?.close({ refocus: false });
  },
});

// The anchored dialog: a .popup with role="dialog" or "alertdialog" in the top
// layer, opened from a trigger it hands the focus back to. Used where a
// control needs more than a menu (a form, a confirm, a sheet of actions) but
// must not push the page around: nothing it shows takes room in the flow.
//
//   <button type="button" id="session-more" …>⋯</button>
//   <div class="[ popup ]" role="alertdialog" id="session-delete" data-trap
//        x-data="popupDialog" x-on:dialog-open.window="openFrom($event)">
//     … <button type="button" data-initial-focus x-on:click="close()">Keep it</button>
//   </div>
//
// Opened by `dialog-open` on window, with { id, anchor }. The focus goes to
// [data-initial-focus], else the first control. Esc closes it and returns the
// focus to the anchor, and goes no further, so a terminal behind it never sees
// it. A dialog also closes on a press outside it; an alertdialog only by its
// own buttons or Esc. data-trap keeps Tab inside. On a phone it is a sheet.

import { defaultEnv, floating } from "./popup.js";

const FOCUSABLE = 'button:not([disabled]), [href], select:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export class AnchoredDialog {
  /**
   * @param {HTMLElement} el  the .popup
   * @param {import("./popup.js").Env} env
   */
  constructor(el, env = defaultEnv()) {
    this.el = el;
    this.env = env;
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
  init() {
    this._dialog = new AnchoredDialog(this.$el, env ?? defaultEnv());
  },
  get isOpen() {
    return this._dialog?.isOpen ?? false;
  },
  /** @param {{ detail?: { id: string, anchor: HTMLElement } }} event */
  openFrom(event) {
    if (event.detail?.id === this.$el.id && event.detail.anchor) this._dialog.open(event.detail.anchor);
  },
  close() {
    this._dialog.close();
  },
  destroy() {
    this._dialog?.close({ refocus: false });
  },
});

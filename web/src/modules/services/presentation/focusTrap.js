// Keeps Tab inside a dialog: from its last control to its first, and back.

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** @param {KeyboardEvent & { currentTarget: HTMLElement }} event a keydown on the dialog */
export function trapTab(event, doc = document) {
  if (event.key !== "Tab") return;
  const items = [...event.currentTarget.querySelectorAll(FOCUSABLE)].filter((el) => !el.checkVisibility || el.checkVisibility());
  if (items.length === 0) return;
  const at = items.indexOf(doc.activeElement);
  if (event.shiftKey && at <= 0) {
    event.preventDefault();
    items[items.length - 1].focus();
  } else if (!event.shiftKey && (at === items.length - 1 || at < 0)) {
    event.preventDefault();
    items[0].focus();
  }
}

// Keeping the keyboard inside a dialog. The delete dialog and the MCP server
// drawer are modal: Tab wraps from the last control to the first and
// Shift+Tab the other way, so the focus never leaves for the page behind.

/** What a person can Tab to. */
export const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Wrap the focus at the ends of the dialog the event passed through. Anywhere
 * else in the dialog the browser moves on its own and nothing is prevented.
 * @param {{ key: string, shiftKey: boolean, currentTarget: { querySelectorAll(selector: string): ArrayLike<any> }, preventDefault(): void }} event
 * @param {{ activeElement: any }} [doc]
 */
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

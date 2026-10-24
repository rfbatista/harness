// Reading and writing the field the pen sits in. The helper writes only when
// the person accepts, and it writes the way typing does, so the form's own
// state follows (an input event) and ⌘Z restores the old text.

const normalize = (text) => text.replace(/\r\n/g, "\n");

/** @param {HTMLInputElement | HTMLTextAreaElement} field */
export function replaceField(field, text) {
  field.focus();
  field.select();
  let done = false;
  try {
    // The browser's own edit: it lands on the undo stack and fires `input`.
    done = text === "" ? document.execCommand("delete") : document.execCommand("insertText", false, text);
  } catch {
    done = false;
  }
  if (!done || normalize(field.value) !== normalize(text)) {
    field.value = text;
    field.dispatchEvent(new Event("input", { bubbles: true }));
  }
}

/** Whether a key goes to a text input, so the digit keys of the title options are typed instead. */
export function isTextTarget(target) {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  if (target instanceof HTMLTextAreaElement) return true;
  if (target instanceof HTMLInputElement) return !["button", "checkbox", "radio", "submit", "reset", "range", "color", "file"].includes(target.type);
  return false;
}

/** Whether an element is rendered (not under display: none). */
export function isShown(el) {
  return typeof el.checkVisibility === "function" ? el.checkVisibility() : el.offsetParent !== null;
}

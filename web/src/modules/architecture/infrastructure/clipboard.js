// Puts text on the clipboard: the async clipboard where the browser offers it
// (a secure page, a user gesture), else a hidden textarea and the copy command.

/** @param {string} text @returns {Promise<boolean>} whether the text was copied */
export async function copyText(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // fall through to the textarea
  }
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.insetBlockStart = "0";
  area.style.opacity = "0";
  document.body.appendChild(area);
  area.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  area.remove();
  return ok;
}

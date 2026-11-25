// Clipboard over the browser's.

import type { Clipboard } from "../application/ports";

export function browserClipboard(nav: Pick<Navigator, "clipboard">): Clipboard {
  return { copy: (text) => nav.clipboard.writeText(text) };
}
